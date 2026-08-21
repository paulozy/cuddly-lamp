# Project Checkpoint - August 20, 2026 (updated)

> **Scope change**: the AI-driven features (code review findings, semantic search,
> code templates) were removed. Documentation generation is the only LLM-backed
> feature left. Migration `023` drops their tables and columns.

## 📌 What Has Been Implemented

### Authentication System ✅
- Email/Password registration with Argon2 hashing (2 iterations, 64MB, 4 parallelism)
- Organization onboarding at registration via `organization_name` and optional `organization_slug`
- Multi-organization login — one org returns tokens directly; multiple orgs return a short-lived `login_ticket` and org choices, completed by `/auth/select-organization`
- JWT access tokens (15min) with JTI revocation tracking
- Refresh token rotation (RFC 9700) — opaque tokens stored as SHA-256, 7-day TTL
- Refresh token reuse detection — replayed token revokes entire family (anti-hijacking)
- Multi-provider OAuth 2.0 Authorization Code Flow (GitHub fully implemented, GitLab ready)
- Stateless HMAC-signed CSRF state tokens (no Redis required)
- Account linking — OAuth auto-links to existing email users
- Role-based access control (admin, maintainer, developer, viewer)
- Token logout — revokes access token + full refresh family

### Repository Management ✅
- CRUD endpoints (`POST/GET/PUT/DELETE /api/v1/repositories`) with ownership enforcement
- **Enriched list response** — newest CI coverage upload per repository via a LATERAL join, in a single query
- Duplicate detection via URL before creation
- GitHub sync on create — fetches branches, commits (last 100), PRs, languages, stars, forks
- Sync status lifecycle: `idle → syncing → synced / error`
- Sync error captured and stored on the repository record
- WebhookConfig — registers GitHub webhook via API, stores HMAC secret per repo
- Webhook registration skipped automatically when `WEBHOOK_BASE_URL` is localhost

### Spatial Repository Navigation ✅
- Canonical `repository_relationships` table for directed repo-to-repo graph edges
- Relationship kinds: `http`, `async`, `library`, `data`, `infra`, `manual`, `other`
- Relationship source/confidence fields support manual curation now and inferred relationships later
- Multiple relationships between the same two repositories are allowed
- Same-organization validation and self-relationship rejection enforced in service layer
- Graph endpoint returns all organization repositories as nodes, including independent repositories
- Legacy `repository_dependencies` is preserved and backfilled into the new graph table by migration `012`

### Webhook Pipeline ✅
- HMAC-SHA256 signature validation (`X-Hub-Signature-256`)
- Idempotency via `X-GitHub-Delivery` — duplicate deliveries return 200 without reprocessing
- Events persisted to `webhooks` table with status (`pending → processed`) and retry metadata
- Background processing via `webhook:process` asynq task
- Supports: `push`, `pull_request`, `issues`, `release`, `repository`, `workflow_run`

### Field-Level Encryption ✅
- **AES-256-GCM cipher** (`internal/crypto/`) for sensitive fields with authenticated encryption
- **Automatic encryption/decryption** via GORM hooks (`BeforeSave`, `AfterFind`)
- **12-byte random nonce** generated per encryption, stored with ciphertext
- **Encrypted fields**: OAuth tokens (`access_token_encrypted` on `oauth_connections`), webhook secrets (`secret_encrypted` on `webhook_configs`)
- **CLI migration tool** (`cmd/migrate-encrypt/`) to encrypt pre-existing plaintext data
- **Key generation**: `openssl rand -base64 32` for 32-byte (256-bit) base64-encoded key via `ENCRYPTION_KEY` env var

### Swagger/OpenAPI Documentation ✅
- **Library**: swaggo/swag (code-first, annotation-based)
- **Format**: OpenAPI 2.0 (Swagger)
- **UI**: Interactive Swagger UI at `/swagger/index.html` via gin-swagger middleware
- **Coverage**: Auth, repository, pull request, webhook, coverage, docs generation, health, and Swagger UI routes documented
- **Annotations**: Complete with `@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Security` markers
- **Security**: JWT BearerAuth scheme documented; webhook HMAC headers documented
- **Generation**: `make swagger` rebuilds docs/ from annotations using pinned `swag@v1.8.12` via `go run`
- **Files**: docs/docs.go committed (for consumers without swag CLI), docs/swagger.json/yaml ignored (.gitignore)

### AI Documentation Generation ✅
- **Pluggable Architecture**: `ai.DocumentationGenerator` in `internal/ai/provider.go`, implemented by Anthropic in `internal/integrations/anthropic/documentation.go`.
- **Doc Worker**: `TypeGenerateDocs` (`docs:generate`) asynq job handler clones the repo, collects context, asks Claude for Markdown, commits files, opens a GitHub PR, and persists content.
- **HTTP Endpoint**: `POST /repositories/:id/docs/generate` queues documentation generation with requested `types` and optional `branch`.
- **Supported Types**: `adr`, `architecture`, `service_doc`, `guidelines`.
- **Generated Files**: ADRs to `docs/adr/README.md`, architecture to `docs/ARCHITECTURE.md`, service docs to `docs/SERVICE.md`, and guidelines to `CONTRIBUTING.md`.
- **GitHub Delivery**: New GitHub client methods create branches, create/update files via Contents API, and open pull requests.
- **Storage**: `doc_generations.content` stores generated Markdown as JSONB for later cross-reference during analysis, with PR URL/number, generated branch, status, tokens, and errors.
- **Token Budget**: Manual trigger checks the shared Anthropic hourly budget before enqueueing.

### Coverage via CI Upload ✅
- **Endpoint**: `POST /api/v1/repositories/:id/coverage`. Bearer `cov_*` token, headers `X-Coverage-Format` + `X-Commit-SHA` (+ optional `X-Coverage-Branch`), raw body up to 5 MB, synchronous 200 response with parsed numbers. Status codes 401 (bad token), 415 (unsupported format), 413 (oversize), 422 (parse failure).
- **Auth**: `coverage_upload_tokens` table. Tokens generated as `cov_<hex>` (32 random bytes), persisted as SHA-256 hash, returned in plaintext only on creation. Scope: single repository. Revocable via DELETE; expiration optional. CRUD via `POST/GET/DELETE /api/v1/repositories/:id/coverage/tokens` (JWT).
- **Storage**: `coverage_uploads` keeps history (last-wins for the patch, every upload preserved). Files-by-path map persisted as JSONB for the PR rule.
- **Read path**: The enriched repository query LATERAL-joins the newest `coverage_uploads` row per repo. Uploads are never mutated — the newest row simply wins.
- **PR rule** (`coverage.PRCoverageGaps`): For PR analyses, files with `status="added"` that are missing from the upload's per-file map (or have `LinesTotal == 0`) generate `severity=medium`, `category=test_coverage` issues. `IsAIGenerated=false`, `Confidence=1.0`. Severity counts run AFTER the append.
- **Per-file granularity**: All four parsers extended to populate `Report.Files []FileCoverage`. Go cover uses `Profile.FileName`; LCOV uses `SF:` blocks; Cobertura merges classes by `filename` attr; JaCoCo joins `package@name` with `sourcefile@name`.
- **Quality score guard**: `GetQualityScore` and DTO `computeQualityScore` skip the coverage deduction unless `coverage_status` is `ok` or `partial` — repos without uploads no longer take a 20-point hit.
- **Worker integration**: New injectable `lookupCoverage` field; default reads from the repository. Resolved at call time (closure) so test mocks with nil receivers don't panic at construction.
- **Files**: `internal/coverage/types.go`, `internal/coverage/parsers.go`, `internal/coverage/pr_rule.go`, `internal/coverage/parsers_test.go`, `internal/coverage/pr_rule_test.go`, `internal/models/coverage_upload.go`, `internal/models/coverage_upload_token.go`, `internal/models/coverage_upload_dto.go`, `internal/models/code_analysis.go` (CoverageStatus field), `internal/models/code_analysis_test.go`, `internal/services/coverage_service.go`, `internal/services/coverage_service_test.go`, `internal/api/handlers/coverage.go`, `internal/api/handlers/coverage_test.go`, `internal/api/factories/make_coverage_handler.go`, `internal/api/routes.go`, `internal/storage/repository.go` (interface), `internal/storage/postgres/coverage_upload_repository.go`, `internal/storage/postgres/coverage_upload_token_repository.go`, `internal/workers/analysis_worker.go`, migrations `017-add-coverage-uploads.sql`, `018-add-coverage-upload-tokens.sql`.

### Infrastructure ✅
- Redis cache layer — `Cache` interface with `ErrCacheMiss`, no-op fallback
- Key builders: `TokenKey`, `UserKey`, `RepoKey`, `SessionKey`
- asynq job queue — `Enqueuer` interface, priority queues (critical/default/low), dead-letter
- Background workers registered in-process: `SyncWorker` (`repo:sync`), `WebhookProcessor` (`webhook:process`), `DocsWorker` (`docs:generate`, `docs:generate_org`)
- GitHub API client (`internal/integrations/github/`) — repos, branches, commits, PRs, Contents API, webhooks
- GORM logger configured: `IgnoreRecordNotFoundError: true`, 200ms slow query threshold
- Server boots without Redis — cache and queue degrade silently to no-op

### Database & Migrations ✅
- 23 SQL migrations applied and tracked via `schema_migrations`
  - `001`: Initial schema — 8 tables + triggers + pgvector
  - `002`: Auth tables — tokens, password_hash
  - `003`: OAuth connections — provider uniqueness, data migration from users
  - `004`: Refresh token rotation — `family_id`, `parent_jti` columns on tokens
  - `005`: Sync status — added `'synced'` to `repositories.sync_status` check constraint
  - `006`: Encrypted fields — `access_token_encrypted` (bytea) on `oauth_connections`, `secret_encrypted` (bytea) on `webhook_configs`
  - `008`: Organizations/multitenancy — organization tables, memberships, organization config
  - `011`: Doc generations — `doc_generations` job metadata, content JSONB, generated branch, PR URL/number, tokens, errors
  - `012`: Repository relationships — spatial graph edges with kind/source/confidence/metadata and legacy dependency backfill
  - `015`: Repository list enrichment indexes — composite index on `repositories(organization_id, created_at DESC)` for the optimized LATERAL join query
  - `016`: Organization output language — BCP 47 tag driving generated-documentation prose
  - `017`–`018`: Coverage uploads and per-repo `cov_*` upload tokens
  - `019`–`020`: Org-scoped doc generations and free-text user prompt
  - `021`–`022`: Repository status columns and CHECK constraint hardening
  - `023`: **AI feature removal** — drops `code_analyses`, `code_templates`, `code_embeddings`, the analysis/embeddings columns on `repositories`, and the Voyage + PR-review columns on `organization_configs`. Destructive and irreversible.
- `StringArray` custom type for PostgreSQL `text[]` columns (implements `driver.Valuer` + `sql.Scanner`)
- Baseline detection for databases pre-dating migration tracking

---

## 📡 API Endpoints

**Public Routes** (`/api/v1`):
| Method | Path | Description |
|--------|------|-------------|
| POST | `/auth/register` | Email/password registration + organization onboarding → JWT pair |
| POST | `/auth/login` | Email/password login → JWT pair or multi-org selection response |
| POST | `/auth/select-organization` | Complete multi-org login with `login_ticket` + `organization_id` |
| POST | `/auth/refresh` | Rotate refresh token → new JWT pair |
| GET | `/auth/:provider` | OAuth redirect (github, gitlab), supports onboarding query params |
| GET | `/auth/:provider/callback` | OAuth callback → JWT pair |
| POST | `/webhooks/github/:repoID` | GitHub webhook receiver (HMAC auth) |
| GET | `/health` | Health check |

**Protected Routes** (`/api/v1`, requires Bearer JWT):
| Method | Path | Description |
|--------|------|-------------|
| POST | `/auth/logout` | Revoke access + refresh family |
| GET | `/users/me` | Current user info |
| POST | `/repositories` | Create repository + trigger sync |
| GET | `/repositories` | List user's repositories |
| GET | `/repositories/graph` | Spatial repository graph with nodes and relationship edges |
| GET | `/repositories/:id` | Get repository by ID |
| PUT | `/repositories/:id` | Update repository |
| DELETE | `/repositories/:id` | Delete repository |
| POST | `/repository-relationships` | Create directed repository relationship |
| PATCH | `/repository-relationships/:id` | Update repository relationship metadata |
| DELETE | `/repository-relationships/:id` | Soft-delete repository relationship |
| GET | `/repositories/:id/pull-requests` | List open GitHub pull requests |
| GET | `/repositories/:id/pull-requests/:pr_number` | Pull request detail with changed files |
| GET | `/repositories/:id/pull-requests/:pr_number/files` | Changed files and patches |
| POST | `/repositories/:id/docs/generate` | Queue AI documentation generation and GitHub PR delivery |
| GET | `/repositories/:id/docs` | List repository documentation |
| PATCH | `/docs/:id` | Edit generated Markdown in place |
| POST | `/repositories/:id/coverage/tokens` | Create a `cov_*` upload token |

---

## 🗄️ Database Schema

| Table | Purpose |
|-------|---------|
| `users` | Platform users with roles, soft deletes |
| `oauth_connections` | OAuth provider links (provider + provider_user_id unique) with encrypted tokens |
| `tokens` | JWT records with revocation, family_id, parent_jti |
| `repositories` | Git repos with sync_status and metadata (JSONB) |
| `repository_relationships` | Directed repo-to-repo graph edges for spatial navigation |
| `webhook_configs` | Per-repo webhook registrations with HMAC secret (encrypted) |
| `webhooks` | Incoming webhook events with status, retry, idempotency |
| `coverage_uploads` | CI coverage reports with per-file granularity, keyed by (repo, commit_sha) |
| `coverage_upload_tokens` | Per-repo `cov_*` tokens, SHA-256 hashed at rest |
| `doc_generations` | AI-generated Markdown docs with content JSONB, generated branch, PR metadata, status, tokens, errors |
| `schema_migrations` | Migration tracking — version + applied_at |

---

## 🔑 Key Implementation Details

### StringArray (PostgreSQL text[])
GORM doesn't natively handle `[]string` → `text[]`. Use `models.StringArray` on any model field mapped to a `text[]` column — it implements `driver.Valuer` and `sql.Scanner` with proper PostgreSQL array literal format.

### Sync Status
Valid values: `idle`, `syncing`, `synced`, `error` (enforced by DB check constraint).

### Webhook Security
- HMAC-SHA256 over raw request body with per-repo secret
- Secret generated with 32 bytes of `crypto/rand`, stored as hex
- Duplicate deliveries detected by `X-GitHub-Delivery` ID before any processing
- Dependency scans are auto-enqueued from webhook processing only when changed files include supported manifest basenames.

### Refresh Token Security (RFC 9700)
- Stored as `SHA-256(raw_token)` — never cleartext
- `family_id` links all rotations of the same session
- Reuse of an already-rotated token revokes the entire family immediately

### Webhook Registration on Localhost
`SyncService.doSync` checks `isLocalURL(webhookBaseURL)` before calling the GitHub API. If the base URL contains `localhost` or `127.0.0.1`, registration is skipped with an info log. Use ngrok for local webhook testing — see `tests/GITHUB_SYNC_TESTING.md`.

### Migration Baseline
If `schema_migrations` is empty but `users` table exists, all current migration files are seeded as applied without executing them. This handles databases created before migration tracking was introduced. **Side effect**: if a new migration file is added before baseline runs, it will be marked applied without executing — apply it manually if this happens.

### Field-Level Encryption
- **Cipher**: AES-256-GCM with 12-byte random nonce per encryption (no separate MAC needed)
- **Storage**: Ciphertext stored as bytea; nonce prepended to ciphertext (25 bytes total minimum: 12 nonce + 1 tag + ciphertext)
- **Transparent hooks**: GORM `BeforeSave` encrypts plaintext fields, `AfterFind` decrypts bytea to plaintext (decryption only happens in memory on fetch, plaintext never stored)
- **Key rotation**: Not yet implemented — future: store key version in database for multi-key support
- **Migration tool** (`cmd/migrate-encrypt/`): Reads plaintext columns, encrypts to new columns, updates models, drops plaintext columns (safe two-phase migration)

### Spatial Repository Navigation
- **Canonical model**: `repository_relationships` stores typed, directed repo-to-repo edges for spatial maps; legacy `repository_dependencies` is only compatibility/backfill input.
- **Graph endpoint**: `GET /api/v1/repositories/graph?kind=http&repository_id=<repo>&include_metadata=true` returns all organization repositories as nodes and matching relationships as edges.
- **Relationship CRUD**: `POST /api/v1/repository-relationships`, `PATCH /api/v1/repository-relationships/:id`, and `DELETE /api/v1/repository-relationships/:id`.
- **Direction**: `source_repository_id -> target_repository_id`; examples include `web-checkout -> payments-api` for HTTP and `payments-api -> shared-libs` for library usage.
- **Kinds**: `http`, `async`, `library`, `data`, `infra`, `manual`, `other`; type-specific details live in JSONB `metadata`.
- **Validation**: Self-relationships and cross-organization relationships are rejected; multiple relationships between the same pair are allowed.

### Auto-Generated Documentation
```
Generate:
  POST /api/v1/repositories/:id/docs/generate
  Body: {"types":["adr","architecture","service_doc","guidelines"],"branch":"main"}
  Creates a pending DocGeneration and enqueues TypeGenerateDocs with manual TaskID deduplication per repository

Worker:
  Shallow-clones the repository, gathers directory tree, key files, and recent commits/PRs, then asks Claude for Markdown
  Creates a docs/auto-generated-{timestamp} branch, commits generated files via GitHub Contents API, and opens a PR

Storage:
  doc_generations.content is JSONB keyed by doc type
  Completed generated docs are rendered into future analysis prompts as PROJECT STANDARDS / DOCUMENTATION

Generated paths:
  adr -> docs/adr/README.md
  architecture -> docs/ARCHITECTURE.md
  service_doc -> docs/SERVICE.md
  guidelines -> CONTRIBUTING.md
```

---

## 📊 Test Coverage

| Package | Coverage | Notes |
|---------|----------|-------|
| `internal/services` | Unit tests ✅ | auth refresh, repository CRUD, relationship graph, sync pipeline, **coverage ingest/token lifecycle** |
| `internal/storage/redis` | Unit tests ✅ | cache get/set/del/exists, no-op fallback |
| `internal/utils` | Unit tests ✅ | URL parsing |
| `internal/coverage` | Unit tests ✅ | Go/LCOV/Cobertura/JaCoCo parsers (per-file granularity) |
| `internal/workers` | Unit tests ✅ | docs worker and webhook processor (push enqueues sync only) |
| `internal/api/handlers` | Unit tests ✅ | docs handler, **coverage ingest + token CRUD** |
| `internal/integrations/anthropic` | Unit tests ✅ | documentation prompts, org context builder, system prompt |
| `internal/storage/postgres` (unit) | Unit tests ✅ | enriched-row mapping, incl. `has_coverage` vs measured 0% |
| `internal/storage/postgres` | Integration tests ⏳ | requires `TEST_DATABASE_URL` |

---

## 📋 Latest Updates (August 20, 2026 — AI removal)

The platform dropped every LLM-backed feature except documentation generation.

**Removed**
- PR review findings — `internal/ai` analysis types, the Anthropic analyzer,
  `analysis_worker`, `code_analyses`, and the GitHub review-posting wrapper.
- Semantic search — `internal/embeddings` (Voyage), the pgvector search query,
  the SSE synthesis stream and its Redis cache.
- Code templates — the `ai.Generator` interface, `template_worker`, `code_templates`.
- `internal/metrics` (clone-based LOC/complexity) — its only consumer was the
  analysis worker.
- Auto-triggers: initial analysis on repository creation, and analysis /
  embedding-indexing on push webhooks. A push now enqueues a sync and nothing else.

**Kept**
- Repository catalog, GitHub sync, webhooks, encryption, auth.
- Pull request browsing — list, detail, changed files with patches, split out
  into a read-only `PullRequestHandler`.
- Spatial repository graph and relationship CRUD.
- Coverage via CI upload.
- AI documentation generation (repo + org scope), including in-app Markdown editing.

**Rewired**
- `SumTokensUsedSince` summed `code_analyses` + `code_templates`; it now sums
  `doc_generations`, the only remaining token consumer.
- The enriched repository query sourced coverage from `code_analyses.metrics`;
  it now LATERAL-joins `coverage_uploads`. `stats.has_coverage` distinguishes
  "never measured" from "measured 0%".

**Migration `023`** drops the tables and columns. It is destructive and
irreversible — dump first if generated findings, templates, or vectors matter.

---

## 🎯 Next Steps

- [x] **Pull request browsing** — read-only GitHub proxy: list, detail, changed files with patches
- [x] **Auto-generated documentation** — Claude Markdown generation, `TypeGenerateDocs` / `TypeGenerateOrgDocs`, GitHub Contents/PR delivery, in-app editing
- [x] **Spatial repository navigation** — typed repo relationship graph, graph endpoint, relationship CRUD, legacy dependency backfill
- [x] **Enriched repository list** — newest coverage upload per repo via LATERAL join, zero N+1 queries
- [x] **Coverage via CI upload** — Codecov-style ingestion endpoint, scoped revocable tokens, per-file granularity
- [x] **AI feature removal** — code review, semantic search and code templates dropped; migration `023`
- [ ] **Handler tests** — broaden unit tests for repository, pull request, and webhook handlers
- [ ] **Postgres integration tests** — wire `TEST_DATABASE_URL` in CI
- [ ] **Key rotation** — store key version in database for multi-key encryption support

---

## 🔧 Environment Variables

```env
# Database
DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME

# Redis (optional — app starts without it)
REDIS_HOST, REDIS_PORT, REDIS_PASSWORD, REDIS_DB

# JWT & Encryption
JWT_SECRET, JWT_ISSUER, JWT_AUDIENCE
ACCESS_TOKEN_TTL=15          # minutes
REFRESH_TOKEN_TTL=10080      # minutes (7 days)
ENCRYPTION_KEY               # Base64-encoded 32-byte key (generate: openssl rand -base64 32)

# GitHub
GITHUB_TOKEN                 # Personal access token (repo + admin:repo_hook scopes; private clones during doc generation)
GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET, GITHUB_CALLBACK_URL
GITHUB_PR_REVIEW_ENABLED     # Post AI-generated PR reviews to GitHub (default: false)

# Webhooks & Public URL
WEBHOOK_BASE_URL             # Public URL for webhook registration (ngrok in local dev)
                             # Leave empty or use localhost to skip GitHub registration

# AI Integration
ANTHROPIC_API_KEY            # Anthropic API key for documentation generation (optional)
ANTHROPIC_TOKENS_PER_HOUR=20000  # Hourly token budget for doc generation
ANTHROPIC_TOKENS_PER_HOUR=20000  # Hourly token budget for Anthropic API

# Logging
LOG_LEVEL                    # debug / info / warn / error
```

---

**Status**: Plain IDP — Auth + Repository Catalog + GitHub Sync + Webhooks + Encryption + Pull Request Browsing + Spatial Repository Graph + Coverage via CI Upload + AI Documentation Generation
**This phase**: removal of the AI-driven features (code review findings, semantic search, code templates) plus the rewiring their removal forced — token budget over `doc_generations`, coverage read from `coverage_uploads`, and a read-only `PullRequestHandler`.
**Total commits (AI + pipeline)**: 18
**Production Readiness**: ~95% (auth + repo + webhook + encryption + AI analysis + semantic search + synthesis streaming + dependency tracking + docs generation + spatial graph + enriched list + coverage upload done; needs broader integration tests, key rotation)

---

## 📖 API Documentation Access

**Interactive Swagger UI:**
```bash
make dev
# Open: http://localhost:3000/swagger/index.html
```

**Documented endpoints include:**
- Auth endpoints (register, login, select organization, refresh, OAuth, logout, /users/me)
- 6 Repository endpoints (CRUD + graph)
- 3 Repository relationship endpoints (create, update, delete)
- 2 Analysis endpoints (trigger, list)
- 2 Semantic search endpoints (generate embeddings, search)
- 2 Dependency endpoints (scan, list)
- 1 Documentation endpoint (generate docs)
- 1 Webhook endpoint (GitHub receiver)
- 1 Health endpoint
- 3 Swagger UI routes

**Features:**
- ✅ JWT security scheme (BearerAuth)
- ✅ Try-it-out functionality (test endpoints from UI)
- ✅ Request/response examples
- ✅ Error codes documented
- ✅ AI analysis endpoints documented with job response models
- ✅ Multi-org login selection response documented
