# Project Checkpoint - April 30, 2026

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
- Duplicate detection via URL before creation
- GitHub sync on create — fetches branches, commits (last 100), PRs, languages, stars, forks
- Sync status lifecycle: `idle → syncing → synced / error`
- Sync error captured and stored on the repository record
- WebhookConfig — registers GitHub webhook via API, stores HMAC secret per repo
- Webhook registration skipped automatically when `WEBHOOK_BASE_URL` is localhost

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
- **Coverage**: Auth, repository, webhook, analysis, dependency, docs generation, template, semantic search, health, and Swagger UI routes documented
- **Annotations**: Complete with `@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Security` markers
- **Security**: JWT BearerAuth scheme documented; webhook HMAC headers documented
- **Generation**: `make swagger` rebuilds docs/ from annotations using pinned `swag@v1.8.12` via `go run`
- **Files**: docs/docs.go committed (for consumers without swag CLI), docs/swagger.json/yaml ignored (.gitignore)

### AI Integration ✅
- **Pluggable Architecture**: `ai.Analyzer`, `ai.DocumentationGenerator`, and `ai.Generator` interfaces in `internal/ai/` — extensible to any LLM (Anthropic, OpenAI, Gemini, etc.)
- **Current Provider**: Anthropic (Claude) via `internal/integrations/anthropic/` using the Anthropic SDK
- **Code Analysis Worker**: `TypeAnalyzeRepo` asynq job handler — repository-wide analysis + PR-specific analysis
- **PR Analysis Mode**: Fetches PR metadata and changed files from GitHub when `PullRequestID > 0`, filters noisy/binary/generated diffs, applies a 50K-token diff budget, and focuses Claude on the PR delta; posts GitHub review comments if `GITHUB_PR_REVIEW_ENABLED=true`
- **Auto-Trigger**: Webhook processor enqueues analysis on `push` events (if not already in progress) + `pull_request` events
- **HTTP Endpoints**: `POST /repositories/:id/analyze` (trigger, 202 Accepted), `GET /repositories/:id/analyses` (list results)
- **Doc-Aware Analysis**: Latest completed generated documentation is injected into Claude prompts as `PROJECT STANDARDS / DOCUMENTATION` so findings can reference ADRs and guidelines.
- **Provider Swap**: To use OpenAI instead of Anthropic — create new struct implementing `ai.Analyzer`, update one line in `main.go`
- **Token Tracking**: Analysis results include model name and token usage for cost monitoring

### Auto-Generated Documentation ✅
- **Pluggable Architecture**: `ai.DocumentationGenerator` in `internal/ai/provider.go`, implemented by Anthropic in `internal/integrations/anthropic/documentation.go`.
- **Doc Worker**: `TypeGenerateDocs` (`docs:generate`) asynq job handler clones the repo, collects context, asks Claude for Markdown, commits files, opens a GitHub PR, and persists content.
- **HTTP Endpoint**: `POST /repositories/:id/docs/generate` queues documentation generation with requested `types` and optional `branch`.
- **Supported Types**: `adr`, `architecture`, `service_doc`, `guidelines`.
- **Generated Files**: ADRs to `docs/adr/README.md`, architecture to `docs/ARCHITECTURE.md`, service docs to `docs/SERVICE.md`, and guidelines to `CONTRIBUTING.md`.
- **GitHub Delivery**: New GitHub client methods create branches, create/update files via Contents API, and open pull requests.
- **Storage**: `doc_generations.content` stores generated Markdown as JSONB for later cross-reference during analysis, with PR URL/number, generated branch, status, tokens, and errors.
- **Token Budget**: Manual trigger checks the shared Anthropic hourly budget before enqueueing.

### Intelligent Code Templates ✅
- **Pluggable Architecture**: `ai.Generator` interface in `internal/ai/generator.go`, separate from `ai.Analyzer` so analysis and generation can evolve independently.
- **Current Provider**: Anthropic (Claude) implements template generation via `internal/integrations/anthropic/generator.go`.
- **Template Worker**: `TypeGenerateTemplate` (`template:generate`) asynq job handler loads organization config, builds repository stack context when available, calls Claude, and persists generated files.
- **Repository-Scoped Generation**: `POST /repositories/:id/templates` uses repository metadata (`languages`, `frameworks`, `topics`, CI/tests) plus optional `stack_hint`.
- **Organization-Level Generation**: `POST /templates` generates from prompt and optional stack hint without repo context.
- **Polling & Reuse**: `GET /templates/:id` polls status/results, `GET /templates` lists org templates, and `PATCH /templates/:id/pin` pins/unpins a reusable team template.
- **Storage**: `code_templates.files` stores generated files inline as JSONB (`path`, `content`, `language`) with summary, stack snapshot, model, token usage, processing time, and error message.
- **Token Budget**: `SumTokensUsedSince` now includes both completed `code_analyses` and completed `code_templates`.
- **Swagger**: All template endpoints include handler annotations and exported request/response DTOs.

### Semantic Code Search ✅
- **Provider Architecture**: `embeddings.Provider` interface isolates provider-specific embedding logic for future swaps.
- **Current Provider**: Voyage AI via `internal/embeddings/voyage.go`, default model `voyage-code-3`, 1024-dimensional vectors.
- **Code Chunking**: `internal/embeddings/chunker.go` clones repositories temporarily, skips generated/binary/vendor/build files, and creates deterministic source-code chunks with line ranges and content hashes.
- **Embedding Worker**: `TypeGenerateEmbeddings` asynq handler in `internal/workers/embedding_worker.go`; batches Voyage requests and replaces stale embeddings per repository/provider/model/dimension/branch.
- **HTTP Endpoints**: `POST /repositories/:id/embeddings` queues indexing; `GET /repositories/:id/search?q=...&min_score=0.55` embeds the query and returns ranked code snippets.
- **Hybrid Ranking**: Search combines pgvector cosine similarity with textual boosts for content, file path, and language, then filters below `min_score` so weak queries can return zero results.
- **pgvector Storage**: Uses `pgvector-go`, cosine candidate ranking, and `code_embeddings` metadata for provider/model/dimension/branch/commit tracking.

### Dependency Tracking ✅
- **Manifest Parsers**: `internal/dependencies/` parses `go.mod`, `package.json`, `requirements.txt`, and `Cargo.toml`.
- **Package Model**: `PackageDependency` stores package name, current/latest version, ecosystem, manifest path, direct dependency flag, vulnerability status, CVEs, update availability, and last scan timestamp.
- **Dependency Worker**: `TypeScanDependencies` (`dependency:scan`) shallow-clones repositories, parses manifests, upserts package rows, sends manifests to Claude for dependency analysis, persists a `CodeAnalysis` record of type `dependency`, and updates vulnerable package status.
- **Claude Dependency Analysis**: Prompts cover known CVEs, outdated packages, license risks, transitive risks, change impact, and recommended versions. `recommended_version` is preserved in issue suggestions.
- **HTTP Endpoints**: `POST /repositories/:id/dependencies/scan` queues manual scans with 10-minute deduplication; `GET /repositories/:id/dependencies?vulnerable=true` lists package inventory.
- **Webhook Auto-Trigger**: Push and PR webhooks enqueue dependency scans only when supported manifest files change.
- **Scope**: Suggestion-based updates only. Recommended versions are stored and can be surfaced in PR review comments; automatic update PR creation is not implemented.

### Infrastructure ✅
- Redis cache layer — `Cache` interface with `ErrCacheMiss`, no-op fallback
- Key builders: `TokenKey`, `UserKey`, `RepoKey`, `SessionKey`
- asynq job queue — `Enqueuer` interface, priority queues (critical/default/low), dead-letter
- Background workers registered in-process: `SyncWorker` (`repo:sync`), `WebhookProcessor` (`webhook:process`), `AnalysisWorker` (`repo:analyze`), `EmbeddingWorker` (`embeddings:generate`), `DependencyWorker` (`dependency:scan`), `DocsWorker` (`docs:generate`), `TemplateWorker` (`template:generate`)
- GitHub API client (`internal/integrations/github/`) — repos, branches, commits, PRs, Contents API, webhooks
- GORM logger configured: `IgnoreRecordNotFoundError: true`, 200ms slow query threshold
- Server boots without Redis — cache and queue degrade silently to no-op

### Database & Migrations ✅
- 11 SQL migrations applied and tracked via `schema_migrations`
  - `001`: Initial schema — 8 tables + triggers + pgvector
  - `002`: Auth tables — tokens, password_hash
  - `003`: OAuth connections — provider uniqueness, data migration from users
  - `004`: Refresh token rotation — `family_id`, `parent_jti` columns on tokens
  - `005`: Sync status — added `'synced'` to `repositories.sync_status` check constraint
  - `006`: Encrypted fields — `access_token_encrypted` (bytea) on `oauth_connections`, `secret_encrypted` (bytea) on `webhook_configs`
  - `007`: Voyage embeddings — provider/model/dimension/branch metadata and `VECTOR(1024)` pgvector storage
  - `008`: Organizations/multitenancy — organization tables, memberships, organization config
  - `009`: Package dependencies — `package_dependencies` inventory with CVE/update metadata
  - `010`: Code templates — `code_templates` generation results with JSONB files and pinning metadata
  - `011`: Doc generations — `doc_generations` job metadata, content JSONB, generated branch, PR URL/number, tokens, errors
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
| GET | `/repositories/:id` | Get repository by ID |
| PUT | `/repositories/:id` | Update repository |
| DELETE | `/repositories/:id` | Delete repository |
| POST | `/repositories/:id/analyze` | Trigger manual code analysis (returns 202 Accepted) |
| GET | `/repositories/:id/analyses` | List code analyses for repository |
| POST | `/repositories/:id/embeddings` | Queue semantic embedding generation |
| GET | `/repositories/:id/search` | Semantic code search over generated embeddings |
| POST | `/repositories/:id/dependencies/scan` | Queue dependency manifest scan |
| GET | `/repositories/:id/dependencies` | List repository package dependencies (`?vulnerable=true`) |
| POST | `/repositories/:id/docs/generate` | Queue AI documentation generation and GitHub PR delivery |
| POST | `/repositories/:id/templates` | Queue repository-scoped AI template generation |
| POST | `/templates` | Queue organization-level AI template generation |
| GET | `/templates/:id` | Poll/retrieve generated template |
| GET | `/templates` | List organization templates (`?pinned=true&status=completed`) |
| PATCH | `/templates/:id/pin` | Pin/unpin template for team reuse |

---

## 🗄️ Database Schema

| Table | Purpose |
|-------|---------|
| `users` | Platform users with roles, soft deletes |
| `oauth_connections` | OAuth provider links (provider + provider_user_id unique) with encrypted tokens |
| `tokens` | JWT records with revocation, family_id, parent_jti |
| `repositories` | Git repos with sync_status, analysis_status, metadata (JSONB) |
| `webhook_configs` | Per-repo webhook registrations with HMAC secret (encrypted) |
| `webhooks` | Incoming webhook events with status, retry, idempotency |
| `code_analyses` | Code review results with issues (JSONB), metrics, model name, token usage |
| `code_embeddings` | Voyage/pgvector source-code embeddings for semantic search |
| `package_dependencies` | Package inventory with ecosystem, manifest path, vulnerability CVEs, update suggestions |
| `code_templates` | AI-generated scaffold templates with JSONB files, status, stack snapshot, and pinning metadata |
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

### Dependency Tracking
- **Supported manifests**: `go.mod`, `package.json`, `requirements.txt`, `Cargo.toml`
- **Upsert key**: `(repository_id, name, ecosystem)` keeps scans idempotent while refreshing versions/status
- **AI request**: Manifest contents are sent as untrusted data under `ai.AnalysisTypeDependency`
- **Vulnerability mapping**: Worker matches AI issues back to package rows by manifest path and package names, extracts `CVE-YYYY-NNNN` values, and stores latest recommended version when present
- **Manual deduplication**: `asynq.TaskID("dependency:scan:manual:{repoID}")` with 10-minute retention returns 409 while a manual scan is already queued/running

### Intelligent Code Templates
- **Generation request**: `prompt` is required; `stack_hint` is optional and can override or refine detected repository stack context.
- **Async state machine**: `pending → generating → completed / failed`; failures persist `error_message` on the `code_templates` row.
- **Manual deduplication**: `asynq.TaskID("template:manual:{templateID}")` with 10-minute retention guards duplicate queueing for the same template record.
- **Stack detection**: Repository-scoped jobs derive `ai.StackProfile` from `Repository.Metadata` and store it in `stack_snapshot`.
- **Generated files**: Stored as JSONB array with `{path, content, language}`; no object storage is required for the current scope.
- **Pinned templates**: `is_pinned`, `name`, `pinned_by_user_id`, and `pinned_at` support team reuse and filtered listing.

### Auto-Generated Documentation
```
Generate:
  POST /api/v1/repositories/:id/docs/generate
  Body: {"types":["adr","architecture","service_doc","guidelines"],"branch":"main"}
  Creates a pending DocGeneration and enqueues TypeGenerateDocs with manual TaskID deduplication per repository

Worker:
  Shallow-clones the repository, gathers directory tree, key files, recent commits/PRs, and latest analysis summary, then asks Claude for Markdown
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
| `internal/services` | Unit tests ✅ | auth refresh, repository CRUD, sync pipeline |
| `internal/storage/redis` | Unit tests ✅ | cache get/set/del/exists, no-op fallback |
| `internal/utils` | Unit tests ✅ | URL parsing |
| `internal/dependencies` | Unit tests ✅ | manifest parser coverage |
| `internal/workers` | Unit tests ✅ | analysis, dependency, docs, and template worker coverage |
| `internal/api/handlers` | Unit tests ✅ | analysis helpers and dependency handler coverage |
| `internal/integrations/anthropic` | Unit tests ✅ | analyzer, documentation, and template generator prompt/parser coverage |
| `internal/storage/postgres` | Integration tests ⏳ | requires `TEST_DATABASE_URL` |

---

## 📋 Latest Updates (April 30, Current Session)

### Phase 1: Analysis Pipeline Deduplication ✅
- **Trigger**: Manual analysis trigger via `POST /repositories/:id/analyze`
- **Implementation**: `asynq.TaskID("analyze:manual:{repoID}")` with `asynq.Retention(10*time.Minute)`
- **Behavior**: Prevents duplicate pending/active analysis jobs; returns 409 Conflict if already exists
- **Tracking**: Added `TriggeredBy` field to `AnalyzeRepoPayload` ("user" | "webhook")
- **Files**: `internal/jobs/tasks/types.go`, `internal/api/handlers/analysis.go`, `internal/api/factories/make_analysis_handler.go`

### Phase 2: Token-Based Rate Limiting ✅
- **Budget**: Hourly token limit (default 20,000, configurable via `ANTHROPIC_TOKENS_PER_HOUR`)
- **Mechanism**: Database SUM query on `code_analyses.tokens_used` where `created_at >= now() - 1h`
- **Applied to**: Both manual triggers and webhook auto-triggers
- **Response**: 429 Too Many Requests when limit exceeded
- **Files**: `internal/config/config.go`, `internal/storage/repository.go`, `internal/storage/postgres/postgres_repository.go`, handlers, `.env.example`

### Phase 3: Local Code Metrics ✅
- **Technology**: go-git shallow clone (`Depth:1`, no submodules) for security
- **Metrics computed**: Lines of code (total, code, blank), cyclomatic complexity estimate
- **Integration**: Calculated before Claude call, passed in prompt with "do not recalculate" instruction
- **Private repositories**: Uses configured `GITHUB_TOKEN` for clone auth and omits `Auth` entirely when the token is empty
- **Graceful degradation**: Continues with zero metrics if clone fails (warns but doesn't fail analysis)
- **Files**: `internal/metrics/calculator.go` (new), `internal/workers/analysis_worker.go`, `internal/ai/provider.go`, `internal/integrations/anthropic/client.go`

### Phase 4: Semantic Code Search ✅
- **Provider**: Voyage AI (`voyage-code-3`, 1024 dimensions) behind `embeddings.Provider`
- **Indexing**: `POST /repositories/:id/embeddings` enqueues `TypeGenerateEmbeddings`
- **Worker**: Temporary git clone, deterministic chunking, batched Voyage document embeddings, pgvector persistence
- **Search**: `GET /repositories/:id/search?q=...&min_score=0.55` embeds query with Voyage, ranks candidates by pgvector cosine similarity, applies textual boosts, and filters below the relevance cutoff
- **Schema**: Migration `007` adds provider/model/dimension/branch/commit metadata and converts embeddings to `VECTOR(1024)`
- **Files**: `internal/embeddings/`, `internal/workers/embedding_worker.go`, `internal/api/handlers/analysis.go`, `migrations/007-add-voyage-embeddings-metadata.sql`

### Phase 5: Dependency Tracking ✅
- **Manifest parsing**: Added parsers/tests for Go modules, npm package manifests, Python requirements, and Cargo manifests
- **Storage**: Added `PackageDependency` model, repository interface methods, Postgres upsert/list/update/delete implementations, and migration `009`
- **Worker**: Added `DependencyWorker` for clone → parse → Claude dependency analysis → persist package and `CodeAnalysis` results
- **Routes**: Added protected dependency scan/list endpoints and factory wiring
- **Webhook integration**: Push/PR events enqueue dependency scans when supported manifest files change
- **Tests**: Added parser tests, Anthropic dependency prompt/response tests, dependency worker test, and dependency handler tests
- **Files**: `internal/dependencies/`, `internal/models/dependency.go`, `internal/workers/dependency_worker.go`, `internal/api/handlers/dependency_handler.go`, `migrations/009-add-package-dependencies.sql`

### Phase 6: Intelligent Code Templates ✅
- **Provider interface**: Added `ai.Generator`, `TemplateRequest`, `TemplateResult`, `GeneratedFile`, and `StackProfile`.
- **Anthropic generation**: Added Claude template prompt construction, JSON response parsing, 8192 max-token generation call, and tests.
- **Storage**: Added `CodeTemplate` model, exported template DTOs, repository interface methods, Postgres CRUD/list implementations, and migration `010`.
- **Worker**: Added `TemplateWorker` for pending/generating/completed/failed transitions, repository stack snapshot extraction, token/model persistence, and tests.
- **Routes**: Added protected template generation, polling, listing, and pin/unpin endpoints with Swagger decorators.
- **Token budget**: Extended hourly Anthropic token sum to include completed template generations.
- **Wiring**: Registered `TypeGenerateTemplate` in task types, route setup, handler factory, and server worker bootstrap.
- **Files**: `internal/ai/generator.go`, `internal/integrations/anthropic/generator.go`, `internal/models/code_template.go`, `internal/models/code_template_dto.go`, `internal/workers/template_worker.go`, `internal/api/handlers/template.go`, `migrations/010-add-code-templates.sql`

### Phase 7: Auto-Generated Documentation + AI Cross-Reference ✅
- **Provider interface**: Added `ai.DocumentationGenerator`, `DocumentationRequest`, `DocumentationResult`, and documentation type constants.
- **Anthropic docs generation**: Added Markdown prompts for ADR, architecture, service docs, and guidelines.
- **Storage**: Added `DocGeneration` model, DTOs, repository interface methods, Postgres CRUD/list implementations, and migration `011`.
- **GitHub delivery**: Added `CreateBranch`, `CreateOrUpdateFile`, and `CreatePullRequest` client methods with tests.
- **Worker**: Added `DocsWorker` for context collection, Claude generation, file commits, PR creation, status transitions, token persistence, and failure recording.
- **Routes**: Added protected `POST /repositories/:id/docs/generate` endpoint and factory wiring.
- **Cross-reference**: `AnalysisWorker` now loads the latest completed docs and injects a concise standards section into analysis prompts.
- **Tests**: Added GitHub Contents/PR tests, Anthropic prompt injection test, analysis request cross-reference test, and docs worker test.
- **Files**: `internal/ai/provider.go`, `internal/integrations/anthropic/documentation.go`, `internal/integrations/github/content.go`, `internal/integrations/github/pull_request_create.go`, `internal/models/doc_generation.go`, `internal/models/doc_generation_dto.go`, `internal/workers/docs_worker.go`, `internal/api/handlers/docs.go`, `migrations/011-add-doc-generations.sql`

---

## 🎯 Next Steps

- [x] **AI Integration** — Claude API for code analysis; pluggable `ai.Analyzer` interface with PR review posting
- [x] **Analysis Pipeline** — Deduplication, token rate limiting, local metrics computation
- [x] **Semantic search** — Voyage AI embeddings, pgvector storage, `TypeGenerateEmbeddings` job, search endpoints
- [x] **Dependency tracking** — package manifest parsing, Claude CVE/update analysis, `TypeScanDependencies`, dependency endpoints
- [x] **Intelligent code templates** — Claude scaffold generation, `TypeGenerateTemplate`, template endpoints, JSONB file storage, pinning
- [x] **Auto-generated documentation** — Claude Markdown generation, `TypeGenerateDocs`, GitHub Contents/PR delivery, doc-aware analysis prompts
- [ ] **Handler tests** — broaden unit tests for repository, analysis, and webhook handlers
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
GITHUB_TOKEN                 # Personal access token (repo + admin:repo_hook scopes; private clones for metrics/dependencies/embeddings)
GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET, GITHUB_CALLBACK_URL
GITHUB_PR_REVIEW_ENABLED     # Post AI-generated PR reviews to GitHub (default: false)

# Webhooks & Public URL
WEBHOOK_BASE_URL             # Public URL for webhook registration (ngrok in local dev)
                             # Leave empty or use localhost to skip GitHub registration

# AI Integration
ANTHROPIC_API_KEY            # Anthropic API key for Claude code, dependency, docs, and template generation (optional)
ANTHROPIC_TOKENS_PER_HOUR=20000  # Hourly token budget for Anthropic API

# Semantic Search
VOYAGE_API_KEY               # Voyage AI key for semantic code search (optional)
EMBEDDINGS_PROVIDER=voyage
EMBEDDINGS_MODEL=voyage-code-3
EMBEDDINGS_DIMENSIONS=1024

# Logging
LOG_LEVEL                    # debug / info / warn / error
```

---

**Status**: 🤖 AI Integration + Semantic Search + Dependency Tracking + Auto Docs Complete (Auth + Repo + Webhook + Encryption + Analysis + Real PR Diffs + Dedup + Rate Limiting + Metrics + Voyage embeddings + package dependency scans + documentation PRs)
**Commits this phase**: 10 planned work units (deduplication, token rate limiting, local metrics, semantic search, metrics clone auth, hybrid semantic relevance, real PR diff analysis, auth onboarding/multi-org login, dependency tracking, auto docs)
**Total commits (AI + pipeline)**: 14
**Production Readiness**: ~93% (auth + repo + webhook + encryption + AI analysis + semantic search + dependency tracking + docs generation done; needs broader integration tests, key rotation)

---

## 📖 API Documentation Access

**Interactive Swagger UI:**
```bash
make dev
# Open: http://localhost:3000/swagger/index.html
```

**Documented endpoints include:**
- Auth endpoints (register, login, select organization, refresh, OAuth, logout, /users/me)
- 5 Repository endpoints (CRUD)
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
