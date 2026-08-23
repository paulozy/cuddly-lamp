# Architecture Decision Records

---

# ADR 001: Multi-Organization Support via Unified JWT with Login Ticket Flow

**Date:** 2024

**Status:** Accepted

## Context

The platform needed to support users who belong to multiple organizations while maintaining stateless JWT authentication. Traditional session-based multi-tenancy approaches were incompatible with the stateless token architecture. Early login attempts revealed that naively embedding organization_id in the JWT would force users to re-authenticate to switch organizations, creating poor UX.

The system also needed to:
- Maintain JWT revocation tracking via JTI (JWT ID) per token
- Support refresh token rotation with family-based reuse detection
- Avoid server-side session state
- Enable secure organization selection without exposing internal IDs

## Decision

Implement a two-phase login flow:

1. **Initial Login Phase:** Email/password authentication succeeds, server checks organization membership count
   - Single org: immediately return access + refresh token with that org embedded
   - Multiple orgs: return `requires_organization_selection=true`, a short-lived HMAC-signed `login_ticket`, and a list of user's organizations

2. **Organization Selection Phase:** User selects from their accessible organizations
   - Client sends `login_ticket` + `organization_id`
   - Server validates ticket HMAC, extracts user_id, verifies user can access that org
   - Returns fresh access + refresh token pair with selected org_id in claims

The login_ticket is HMAC-SHA256 signed (using JWT_SECRET) with expiry, allowing stateless validation without database lookup.

## Consequences

**Positive:**
- Users can switch organizations without re-entering credentials
- Zero server-side session state; validation is cryptographic
- Login flow remains stateless and horizontally scalable
- Seamless UX for single-org users (transparent fast path)
- Integrates naturally with refresh token rotation (new orgs get new token families)

**Negative:**
- Two round-trips required for multi-org users
- HMAC ticket generation adds minimal crypto overhead per multi-org login
- Frontend must handle the `requires_organization_selection` response shape
- Token size grows slightly with org_id claim

**Mitigations:**
- Tickets expire quickly (5-10 minutes typical)
- Login ticket HMAC uses same secret as JWT, reducing key rotation complexity

---

# ADR 002: Field-Level AES-256-GCM Encryption for Sensitive Data at Rest

**Date:** 2024

**Status:** Accepted

## Context

The platform stores sensitive provider credentials (OAuth access/refresh tokens, GitLab personal access tokens, webhook secrets) directly in PostgreSQL. Prior to this decision, these fields were stored in plaintext, creating a significant security exposure if the database were ever compromised or migrated to an untrusted environment.

The team needed:
- Transparent encryption/decryption in application code (no query-level changes)
- Support for existing unencrypted data (backward compatibility during rollout)
- Key rotation capability for future compliance requirements
- Single 32-byte key management (no per-record key derivation)

## Decision

Implement transparent field-level encryption using AES-256-GCM via GORM hooks:

1. **Encryption Layer:** Create a custom GORM type wrapper that intercepts `BeforeSave` and `AfterFind` hooks
2. **Key Format:** Base64-encoded 32-byte key loaded from `ENCRYPTION_KEY` environment variable
3. **Algorithm:** AES-256-GCM (authenticated encryption, prevents tampering)
4. **Fields Encrypted:**
   - `oauth_connections.access_token_encrypted`
   - `oauth_connections.refresh_token_encrypted`
   - `webhooks.secret_encrypted` (for GitLab webhooks)
5. **Migration Tool:** `cmd/migrate-encrypt/` CLI scans existing unencrypted records and encrypts them in-place without downtime

The approach uses a nonce for each encryption (included in ciphertext prefix), ensuring same plaintext encrypts differently each time.

## Consequences

**Positive:**
- Database dump is no longer a credential leak
- Encryption is transparent to business logic (no refactoring of queries)
- Backward compatible: unencrypted fields skip decryption gracefully
- Single key per environment simplifies key management
- GCM mode provides both confidentiality and integrity
- Migration tool enables zero-downtime rollout

**Negative:**
- All encrypted fields become VARCHAR(MAX) in schema (stores base64 ciphertext + nonce)
- Key rotation requires re-encrypting all records (not implemented; future work)
- Searchability on encrypted fields is impossible (use plaintext indexes for provider_id instead)
- Small performance overhead on every read/write of encrypted fields (GORM hooks)

**Mitigations:**
- AES-256-GCM is fast; overhead is negligible for typical query patterns
- Most queries filter by unencrypted fields (user_id, org_id, provider)
- Key leakage severity remains lower than plaintext; meets compliance baselines

---

# ADR 003: Repository Relationships as a Canonical Directed Graph Model

**Date:** 2024

**Status:** Accepted

## Context

The platform needed to support spatial repository navigation — visualizing how repositories depend on, call, or relate to one another. Initial implementation used a `repository_dependencies` table with implicit semantics (assumed all were library/import relationships).

As the platform evolved, stakeholders needed:
- Multiple relationship types (HTTP APIs, async messaging, library imports, data flows, infrastructure, manual links)
- Metadata on relationships (protocol, endpoint, message type)
- Confidence/source tracking (inferred vs. manually curated)
- Graph traversal for impact analysis ("if we change this repo, which others break?")
- Legacy data migration without losing existing dependency records

## Decision

Replace implicit `repository_dependencies` with an explicit `repository_relationships` table as the canonical graph model:

**Schema:**
```sql
CREATE TABLE repository_relationships (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  source_repository_id UUID NOT NULL REFERENCES repositories(id),
  target_repository_id UUID NOT NULL REFERENCES repositories(id),
  kind TEXT NOT NULL CHECK (kind IN ('http', 'async', 'library', 'data', 'infra', 'manual', 'other')),
  label TEXT,
  metadata JSONB,
  source TEXT DEFAULT 'manual',    -- 'manual' | 'inferred' | 'import_analysis'
  confidence DECIMAL(3,2),         -- 0.0 to 1.0, null for manual
  created_at TIMESTAMP,
  updated_at TIMESTAMP,
  UNIQUE (organization_id, source_repository_id, target_repository_id, kind)
);
```

**Graph Endpoint:** `GET /api/v1/repositories/graph` returns all repositories as nodes, all relationships as edges, serialized as a JSON graph structure.

**Migration Path:** Migration 012 backtracks `repository_dependencies` into `repository_relationships` rows with `kind='library'`, `source='inferred'`.

## Consequences

**Positive:**
- Explicit relationship kinds replace implicit assumptions
- Metadata (protocol, endpoint) enables detailed navigation without secondary API calls
- Confidence scores allow filtering inferred vs. curated relationships
- Graph traversal algorithms can be implemented on clients or via future server endpoints
- Multiple edges between same repos (e.g., both HTTP *and* library import) are supported
- Legacy data preserved and enriched

**Negative:**
- Schema change required; existing code querying `repository_dependencies` must migrate
- Graph endpoint returns full adjacency list; pagination not implemented (assumes < 1000 repos per org)
- Metadata is free-form JSONB; no schema validation on valid fields per kind
- No cascade delete from `repository_relationships` to `repositories` (dangling edges possible if repo deleted)

**Mitigations:**
- Soft deletes on repositories (deleted_at flag) prevent true foreign key violations
- UNIQUE constraint prevents duplicate edges of the same kind
- Kinds enum is strict; invalid kinds rejected at insert time
- Metadata validation can be added to API handlers; JSONB schema enforcement is future work
- Pagination deferred until graph size exceeds 1000 nodes (current scale known safe)

---

# ADR 004: Async Job Queue (asynq) for Background Processing with Redis Graceful Degradation

**Date:** 2024

**Status:** Accepted

## Context

The platform performs long-running operations that should not block HTTP responses:
- Repository synchronization from GitHub/GitLab (cloning, listing PRs/MRs)
- Documentation generation via Claude (LLM inference, GitHub PR creation)
- Webhook event processing (validate, fetch artifacts, update state)
- Coverage report parsing and ingestion

Initial synchronous approach blocked HTTP handlers; timeouts and poor UX resulted. The team needed:
- Task queueing with retries and backoff
- Priority queues (urgent webhook processing vs. background doc generation)
- Scheduled tasks (periodic sync cron jobs)
- Graceful degradation if Redis is unavailable (fallback to inline execution)

## Decision

Integrate `hibiken/asynq` as the job queue, backed by Redis:

1. **Enqueuer Interface:** Abstract job submission behind a `Enqueuer` interface
   - Production: `asynq.Client` (submits to Redis)
   - Test/Fallback: `NoOpEnqueuer` (returns immediately, no-op)

2. **Worker Setup:**
   - `SyncWorker`: handles `repo:sync` tasks (clones repo, updates metadata via provider APIs)
   - `WebhookProcessor`: handles `webhook:process` tasks (validates, updates repo state)
   - `DocsWorker`: handles `docs:generate` tasks (invokes Claude, commits to repo, opens PR)

3. **Lifecycle:**
   - Jobs enqueued during HTTP handler execution
   - HTTP responds immediately (202 Accepted or task ID returned)
   - Worker pool processes queued tasks in background with exponential backoff retries
   - Server graceful shutdown: waits for in-flight tasks to complete (5s timeout)

4. **Degradation:** If Redis is unavailable (connection fails), `Enqueuer` returns a no-op; tasks are silently skipped (logged as warnings). Sync/webhook processing degrades to inline execution on next manual trigger.

## Consequences

**Positive:**
- HTTP responses return quickly; long operations don't block clients
- Retries with exponential backoff handle transient failures (network glitches, rate limits)
- Priority queues ensure webhooks (real-time) outpace background docs (best-effort)
- Scheduled tasks enable periodic repository re-sync (future: every 6h)
- Redis connection pooling is efficient; minimal overhead per enqueue
- Dead-letter queue captures permanently failed tasks for debugging

**Negative:**
- Adds operational dependency on Redis; full service unavailability if Redis fails
- Task status visibility limited (asynq has no built-in callback; must poll HTTP endpoint for job status)
- Graceful degradation (no-op on Redis failure) means missing tasks are silent; monitoring required
- Task payload serialization (JSON) has size limits; very large repos may hit limits

**Mitigations:**
- Redis is already required for caching; reusing it reduces operational surface
- Task status stored in PostgreSQL `doc_generations`, `webhook_events` rows; HTTP endpoint can query those
- Alarms on Redis unavailability alert ops before silent task loss affects users
- Large repo handling: stream file reading, emit task updates as chunks (future optimization)
- Graceful no-op is acceptable for background tasks; webhook processing is critical but can be re-triggered manually

---

# ADR 005: Pluggable AI Documentation Generator Interface with Anthropic Implementation

**Date:** 2024

**Status:** Accepted

## Context

The platform added AI-generated documentation as an opt-in feature, allowing repositories to generate ADRs, architecture guides, and service documentation via Claude. However, early implementation tightly coupled Claude SDK calls to business logic, making it hard to:
- Test without hitting Anthropic's API (expensive, slow)
- Swap providers in future (OpenAI, local models)
- Skip documentation generation if API key is missing
- Control token budget and rate limiting across an organization

## Decision

Define a `DocumentationGenerator` interface as the sole abstraction point for LLM-backed features:

```go
type DocumentationGenerator interface {
    GenerateDocumentation(ctx context.Context, req GenerateDocRequest) (GenerateDocResponse, error)
}
```

**Implementation:**
1. **Anthropic Implementation:** Uses `anthropics/anthropic-sdk-go` with configurable model (Claude 3.5 Sonnet)
2. **Token Budget Tracking:** `doc_generations` table tracks token usage per organization per hour
3. **No-Op Fallback:** If `ANTHROPIC_API_KEY` is unset, a `NoOpDocumentationGenerator` is registered; doc generation requests return 501 Not Implemented with a helpful message
4. **Prompt Engineering:** System prompt includes organization output language (BCP 47, e.g., `en`, `pt-BR`) via `OrganizationConfig.OutputLanguage`, ensuring generated Markdown is localized

**Token Budgeting:**
- `ANTHROPIC_TOKENS_PER_HOUR` env var sets hourly limit (default 20K)
- Before each generation, sum tokens used in past hour from `doc_generations` table
- If remaining budget < estimated tokens, reject with 429 Too Many Requests
- After generation, record actual tokens used in `doc_generations.tokens_used`

## Consequences

**Positive:**
- Single point of abstraction; easy to swap or test providers
- Optional feature: no API key required for platform operation
- Token budgeting prevents unexpected bills from malicious or runaway generations
- Localization via system prompt is elegant; no post-generation translation needed
- No-Op fallback allows graceful degradation (clear error message vs. silent failure)
- All doc generation feature-gated behind single interface

**Negative:**
- `DocumentationGenerator` only abstracts generation; GitHub/GitLab integration (cloning, branch creation, PR opening) remains tightly coupled to handlers
- Token budget is approximate; Claude's actual token count may differ from estimate (no pre-flight token counting)
- Organization output language requires manual `PATCH /organizations/configs` — not auto-detected from user locale
- Hourly window is UTC-based; midnight UTC boundary may surprise users in other timezones

**Mitigations:**
- GitHub/GitLab logic abstraction deferred (future ADR if multi-provider git ops grow)
- Token budget conservatively estimates; overage results in skipped generation (acceptable for cost control)
- Default `OutputLanguage` is English; orgs can opt into localization via config endpoint
- Token budget resets on calendar hours (00:00 UTC); unclear but predictable; document behavior