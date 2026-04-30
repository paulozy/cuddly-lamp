# Project: IDP with AI Backend

## Overview

Backend of an Identity Provider (IDP) platform that integrates AI for code analysis. Provides JWT-based authentication, OAuth integration (GitHub/GitLab), repository management, and semantic code search powered by embeddings.

## Tech Stack

- **Language**: Go 1.21+
- **Framework**: Gin (HTTP routing & middleware)
- **Database**: PostgreSQL 14+ with pgvector extension
- **Cache**: Redis (optional) via `go-redis/v9` — `internal/storage/redis/`
- **Job Queue**: `asynq` (Redis-backed) — `internal/jobs/`
- **Testing**: Go test (standard library)
- **Deploy**: Docker Compose (local dev), Docker (production-ready)
- **ORM**: GORM v2
- **Auth**: JWT (golang-jwt/jwt v5)
- **Password Hashing**: Argon2 (golang.org/x/crypto/argon2)
- **Encryption**: AES-256-GCM (crypto/aes, crypto/cipher)
- **AI Integration**: Anthropic API (Claude for code analysis)
- **Embeddings**: Voyage AI (`voyage-code-3`) + pgvector for semantic code search

## Architecture

```
internal/
├── api/
│   ├── handlers/         # HTTP request handlers (auth, repository, webhook)
│   ├── middleware/       # JWT auth, CORS, logging
│   ├── routes.go         # Route definitions
│   └── factories/        # Dependency injection setup
├── embeddings/           # Embedding provider abstraction + Voyage implementation + code chunking
├── integrations/
│   └── github/           # GitHub API client + webhook HMAC validation
├── models/               # GORM models (User, Repository, Token, WebhookConfig, etc.)
├── services/             # Business logic (AuthService, RepositoryService, SyncService)
├── workers/              # asynq task handlers (SyncWorker, WebhookProcessor, AnalysisWorker, EmbeddingWorker)
├── storage/
│   ├── postgres/         # PostgreSQL repository implementation
│   └── redis/            # Redis client, Cache interface, key builders
├── jobs/                 # Background job queue (asynq) — Enqueuer, Worker, task types
├── config/               # Configuration loading from .env
└── utils/                # Logging, URL parsing helpers
```

## Coding Conventions

- Use **PascalCase** for types, interfaces, and exported functions
- Use **camelCase** for unexported functions and variables
- Use **snake_case** for database column names (handled by GORM struct tags)
- Prefer **composition over inheritance**
- Import order: stdlib > external > internal
- Max 150 lines per function (guideline, not strict)
- Use context.Context as first parameter in all I/O functions

## Testing Rules

- **All new code MUST have tests** — even small changes
- Test framework: Go's standard `testing` package
- Naming convention: `TestFunctionName` and `func TestFunctionName_ShouldBehavior(t *testing.T)`
- Mocks only for external I/O: APIs, database, file system
- Use table-driven tests for multiple scenarios
- Run `go test ./...` before considering a task complete
- Database tests should use PostgreSQL test instance (docker-compose)

## Git Conventions

- Commits in **English**, format: `type(scope): description`
- Types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `debug`
- Branches: `feat/name`, `fix/name`, `refactor/name`
- Commit footer: `Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com>`
- Never commit secrets, `.env`, or credentials
- Example: `fix(auth): ensure UTC timezone handling in token generation`

## Current Focus

1. **Authentication & JWT**: Complete — email/password onboarding creates organizations, login supports multi-org selection tickets, JWT access tokens, refresh token rotation (RFC 9700), OAuth (GitHub/GitLab)
2. **Infrastructure**: Complete — Redis cache layer + asynq job queue wired in `main.go` with no-op fallbacks
3. **Repository management**: Complete — CRUD endpoints, GitHub sync (branches, commits, PRs, languages), WebhookConfig registration
4. **Webhook pipeline**: Complete — HMAC-validated ingestion, idempotency via delivery ID, background processing worker
5. **Field-level encryption**: Complete — AES-256-GCM encryption for sensitive fields (OAuth tokens, webhook secrets), transparent GORM hooks, CLI migration tool
6. **API Documentation**: Complete — Swagger/OpenAPI 2.0 with swaggo/swag, committed docs at `/swagger/index.html`; `make swagger` uses pinned `swag@v1.8.12`
7. **AI Integration**: Complete — Pluggable `ai.Analyzer` interface, Claude (Anthropic) implementation, code analysis worker, real PR diff fetching/filtering/budgeting + optional review posting, auto-trigger on webhooks
8. **Analysis Pipeline Improvements**: Complete — Deduplication (manual triggers), token rate limiting (20K/hour), local metrics computation
9. **Semantic Search**: Complete — Voyage AI embeddings, `TypeGenerateEmbeddings` worker, hybrid pgvector/text ranking, protected indexing/search endpoints

## Known Issues & Constraints

- **Partial test coverage**: services and redis have tests; postgres integration tests and handlers have none yet
- **Token validation**: Allows tokens not found in DB for backward compatibility (tokens created before DB migration)
- **Soft deletes**: Using `*time.Time` for DeletedAt (nullable), not `gorm.DeletedAt`
- **Worker in-process**: asynq worker runs in the same binary as the HTTP server; split to `cmd/worker/` when independent scaling is needed
- **Timezone handling**: PostgreSQL TIMESTAMP (no timezone) requires explicit UTC conversion in Go — always use `.UTC()`
- **StringArray**: `models.StringArray` is a custom type for PostgreSQL `text[]` — use it instead of `[]string` on any GORM model field mapped to a `text[]` column
- **Webhook registration on localhost**: skipped automatically when `WEBHOOK_BASE_URL` contains `localhost`/`127.0.0.1` — use ngrok for local webhook testing
- **Field-level encryption**: Encrypted fields require `ENCRYPTION_KEY` at startup; existing unencrypted data must be migrated using `cmd/migrate-encrypt/` tool; decryption happens transparently via GORM `AfterFind` hooks
- **Swagger docs**: Generated by `swag init` from annotations in handler code; regenerate with `make swagger` or `go run github.com/swaggo/swag/cmd/swag@v1.8.12 init -g cmd/server/main.go -o docs --parseInternal --parseDependency`
- **Swagger CLI**: `swag` is not required globally; `make swagger` invokes the pinned CLI through `go run`. In restricted sandboxes this can fail until network/module cache access is available.

## Authentication & Organization Notes

- **Initial registration**: Public `POST /api/v1/auth/register` accepts user fields plus `organization_name` and optional `organization_slug`; if slug is omitted it is derived from the organization name.
- **First member role**: The first user in an organization becomes `admin`; later members default to `developer`.
- **Login without org slug**: Public `POST /api/v1/auth/login` accepts only email/password in the primary flow. If the user belongs to one organization, the API returns a normal `TokenResponse`.
- **Multi-org login**: If the user belongs to multiple organizations, login returns `202 Accepted` with `requires_organization_selection`, a short-lived signed `login_ticket`, and the available organizations. The frontend completes login via `POST /api/v1/auth/select-organization` with `login_ticket` + `organization_id`.
- **Legacy org-scoped auth routes**: `/api/v1/orgs/:slug/auth/...` remain for backward compatibility, but new clients should use `/api/v1/auth/...`.
- **OAuth onboarding**: Public OAuth start routes accept `organization_name` and optional `organization_slug` in query params so the callback can create/resolve the org from signed state.

## Database Notes

- PostgreSQL TIMESTAMP columns (no timezone info) require explicit UTC handling
- `time.Now()` returns local timezone — always use `time.Now().UTC()` before storing
- GORM auto-migration creates columns without timezone, so explicit UTC conversion is critical
- Column name mapping uses GORM struct tags: `gorm:"column:name"` (required for non-standard names like GitHubID → github_id)

## Encryption Notes

- **AES-256-GCM cipher**: Provides authenticated encryption (no separate MAC needed)
- **Key generation**: `openssl rand -base64 32` produces a 32-byte (256-bit) key, base64-encoded
- **Nonce (IV)**: 12-byte random nonce generated fresh per encryption; stored as ciphertext prefix
- **Decryption flow**: GORM `AfterFind` hook extracts nonce, decrypts, stores plaintext in memory model
- **Encryption flow**: GORM `BeforeSave` hook reads plaintext, encrypts, stores ciphertext in database
- **Encrypted fields**: OAuth tokens (`access_token_encrypted`), webhook secrets (`secret_encrypted`)
- **Migration**: Use `cmd/migrate-encrypt/main.go` to encrypt pre-existing plaintext data (reads from old plaintext columns, writes encrypted versions, updates foreign keys, deletes plaintext columns)
- **Key rotation**: Not yet implemented; new `ENCRYPTION_KEY` will fail to decrypt existing ciphertext. Plan: store key version in database for multi-key support.

## AI Integration Notes

- **Architecture**: Pluggable `ai.Analyzer` interface in `internal/ai/provider.go` — extensible to any LLM provider (Anthropic, OpenAI, Gemini, etc.)
- **Current Implementation**: Anthropic (Claude) via `internal/integrations/anthropic/client.go` — Anthropic SDK with structured prompts
- **Swapping Providers**: Create new struct implementing `ai.Analyzer`, update one line in `cmd/server/main.go` (where `anthropic.NewClient()` is called) — no other changes needed
- **Analysis Request**: Sent to Claude with repository metadata (languages, commits, test coverage), computed metrics (LOC, complexity), optional budgeted PR diffs for code review mode
- **Analysis Response**: Parsed JSON with code issues (severity, file path, line, message), metrics (complexity, test coverage), and model name/token usage
- **PR Analysis Mode**: Triggered when `PullRequestID > 0` in task payload; fetches PR metadata/files from GitHub, filters noisy/binary/generated diffs, applies a 50K-token diff budget, skips whole-repo metrics/recent commits, and focuses Claude only on shown changed files.
- **Auto-Trigger**: Webhook processor enqueues `TypeAnalyzeRepo` on `push` events (if `AnalysisStatus != "in_progress"`) and on `pull_request` events (always, with PR ID)
- **Deduplication**: Manual trigger deduplication via `asynq.TaskID("analyze:manual:{repoID}")` with 10-minute retention — returns 409 Conflict if already pending
- **Rate Limiting**: Token-based rate limiting (default 20K tokens/hour, configurable via `ANTHROPIC_TOKENS_PER_HOUR`) — checks accumulated tokens in last hour via DB SUM query
- **Local Metrics**: Computed before Claude call via shallow git clone (`Depth:1`) with go-git, no submodules — counts lines of code, estimates cyclomatic complexity, and uses configured `GITHUB_TOKEN` for private repository access.
- **Future Enhancements**: configurable analysis types ("code_review", "security", "architecture")

## Semantic Search Notes

- **Architecture**: `embeddings.Provider` interface in `internal/embeddings/provider.go` keeps provider-specific code isolated; the MVP implements Voyage only.
- **Current Provider**: Voyage AI via `internal/embeddings/voyage.go`, default model `voyage-code-3`, default dimension `1024`.
- **Indexing Flow**: `EmbeddingWorker` handles `TypeGenerateEmbeddings`; it clones the target repository temporarily using configured `GITHUB_TOKEN` when present, chunks source files deterministically, sends batches to Voyage with `input_type=document`, and stores vectors in `code_embeddings`.
- **Search Flow**: `GET /api/v1/repositories/:id/search?q=...&min_score=0.55` embeds the query with `input_type=query`, ranks stored code chunks with pgvector cosine distance, applies textual boosts for `content`, `file_path`, and `language`, then filters out matches below `min_score`.
- **Endpoints**: `POST /api/v1/repositories/:id/embeddings` enqueues indexing; `GET /api/v1/repositories/:id/search` returns matching file snippets with line range and similarity score.
- **Storage**: `code_embeddings.embedding` is `VECTOR(1024)` using `pgvector-go`; rows include provider, model, dimension, branch, commit SHA, content hash, file path, language, and line range.
- **Relevance Controls**: Semantic search defaults to `min_score=0.55`; callers can tune `min_score` from `0` to `1`. Low-confidence searches can legitimately return `total: 0`.
- **Provider Swap**: Add a new implementation of `embeddings.Provider`, extend config/bootstrap provider selection in `cmd/server/main.go`, and keep worker/handler/storage unchanged.

## Swagger/OpenAPI Documentation

- **Library**: swaggo/swag v1.8.12 (code-first, annotation-based)
- **Format**: OpenAPI 2.0 (Swagger)
- **UI**: gin-swagger serving `/swagger/*any` route (Swagger UI embedded)
- **Generation**: `make swagger` or `go run github.com/swaggo/swag/cmd/swag@v1.8.12 init -g cmd/server/main.go -o docs --parseInternal --parseDependency`
- **Generated files**: `docs/docs.go` (committed), `docs/swagger.json` and `docs/swagger.yaml` (ignored in .gitignore)
- **Annotations**: Auth, repository, webhook, analysis, semantic search, health, and Swagger UI routes are documented with `@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Security` markers
- **General API Info**: Defined in comments above `func main()` in `cmd/server/main.go` — includes title, version, description, host, base path, security definitions
- **Security**: BearerAuth scheme documented for JWT-protected endpoints; header parameters documented for webhook HMAC validation
- **Regeneration**: After adding/modifying handler annotations, run `make swagger` to regenerate docs. This downloads/runs pinned `swag@v1.8.12` if it is not already in the Go module cache.

## Environment Configuration

`.env` variables (see `.env.example`):
- `DB_*`: PostgreSQL connection
- `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`, `REDIS_DB`: Redis (optional — app starts without it)
- `JWT_SECRET`, `JWT_ISSUER`, `JWT_AUDIENCE`: JWT configuration
- `ACCESS_TOKEN_TTL`, `REFRESH_TOKEN_TTL`: Token expiration (in minutes)
- `ENCRYPTION_KEY`: Base64-encoded 32-byte AES-256-GCM key for field encryption (generate with `openssl rand -base64 32`)
- `ANTHROPIC_API_KEY`, `GITHUB_TOKEN`: External API keys; `GITHUB_TOKEN` is also used for private repository clones during metrics and embedding generation.
- `VOYAGE_API_KEY`: Voyage AI key for semantic code search (optional — skips embedding worker/search if not set)
- `EMBEDDINGS_PROVIDER`: Embedding provider selector, default `voyage`
- `EMBEDDINGS_MODEL`: Embedding model, default `voyage-code-3`
- `EMBEDDINGS_DIMENSIONS`: Embedding vector dimension, default `1024`
- `WEBHOOK_BASE_URL`: Public base URL for webhook registration (e.g. ngrok URL); omit or use localhost to skip GitHub webhook registration
- `LOG_LEVEL`: Logging verbosity (info, debug, error)

## Do NOT

- Do not create documentation files unless asked
- Do not add dependencies without confirming first
- Do not change architecture without prior discussion
- Do not ignore failing tests (run before task completion)
- Do not over-engineer — solve the current problem, not hypothetical ones
- Do not use weak password hashing (Argon2 is required)
- Do not mix UTC and local time — always be explicit with `.UTC()`
- Do not skip error handling at system boundaries (API input, DB, external services)

## Checkpoint — April 29, 2026

**Status**: ✅ All core features complete — Auth + Repo Sync + Webhook Pipeline + Encryption + AI Integration

**Completed in previous session** (April 28):
1. ✅ **Field-level encryption** (`internal/crypto/`):
   - AES-256-GCM cipher with 12-byte random nonce per encryption
   - Transparent GORM hooks (`BeforeSave`, `AfterFind`) for automatic encryption/decryption
   - `Serializer` interface for field-level encryption on models

2. ✅ **CLI migration tool** (`cmd/migrate-encrypt/`):
   - Reads plaintext fields from database
   - Encrypts and writes to encrypted columns
   - Handles both `oauth_connections` and `webhook_configs` tables

**Completed in this session** (April 29):
1. ✅ **Pluggable AI provider interface** (`internal/ai/`):
   - `ai.Analyzer` interface with `AnalyzeCode()` and `Provider()` methods
   - Request types: `AnalysisRequest` with repo metadata and optional PR diffs
   - Response types: `AnalysisResult` with code issues, metrics, model info, token usage
   - `mock_analyzer.go` for testing

2. ✅ **Anthropic (Claude) implementation** (`internal/integrations/anthropic/`):
   - Raw HTTP client implementing `ai.Analyzer` interface
   - Uses `claude-haiku-4-5-20251001` model (cost-effective for analysis)
   - Structured prompts built from `AnalysisRequest` metadata
   - JSON response parsing into `AnalysisResult` with token tracking
   - Full test coverage with mock responses

3. ✅ **Code analysis worker** (`internal/workers/analysis_worker.go`):
   - `TypeAnalyzeRepo` job handler following `sync_worker` pattern
   - Supports two modes: repository-wide analysis + PR-specific analysis
   - Repository analysis: fetches commits, calls analyzer, saves `CodeAnalysis` record
   - PR analysis: fetches PR diffs, analyzes changed files, posts GitHub review (if enabled)
   - Updates `repository.AnalysisStatus` and `LastAnalyzedAt` timestamps

4. ✅ **GitHub PR operations** (`internal/integrations/github/pr.go`):
   - `GetPullRequest()`: fetch PR metadata (title, body, state, author)
   - `GetPullRequestFiles()`: get changed files with diffs
   - `CreatePullRequestReview()`: post review comments to GitHub PR (optional, gated by `GITHUB_PR_REVIEW_ENABLED`)
   - Diff position calculation for line-specific comments

5. ✅ **HTTP endpoints** (`internal/api/handlers/analysis.go`):
   - `POST /api/v1/repositories/:id/analyze`: trigger manual analysis (returns 202 Accepted with job ID)
   - `GET /api/v1/repositories/:id/analyses`: list analyses for repository
   - Request validation: repository existence, optional branch/commit override
   - Factory pattern DI (`make_analysis_handler.go`)

6. ✅ **Webhook auto-trigger** (`internal/workers/webhook_processor.go`):
   - Push events: enqueue `TypeAnalyzeRepo` if `AnalysisStatus != "in_progress"`
   - PR events: always enqueue `TypeAnalyzeRepo` with `PullRequestID` for PR analysis
   - Prevents duplicate analysis via status checks

7. ✅ **Configuration & wiring** (`cmd/server/main.go`):
   - Conditional Anthropic client creation (if `ANTHROPIC_API_KEY` set)
   - Analysis worker registration with job queue
   - Graceful degradation: no-op enqueuer if Anthropic key missing

8. ✅ **Documentation updates**:
   - `.env.example`: added `ANTHROPIC_API_KEY`, `GITHUB_PR_REVIEW_ENABLED`, `WEBHOOK_BASE_URL` with descriptions
   - `README.md`: AI Integration features, project structure (new `internal/ai/` + `internal/integrations/anthropic/`), updated endpoint count (17 total), marked task as complete
   - `CLAUDE.md`: tech stack includes Anthropic, Current Focus updated, added "AI Integration Notes" section with pluggability architecture, updated Swagger endpoint count

**Completed in current session** (April 29, continued):
1. ✅ **Analysis Pipeline Deduplication** (`internal/jobs/tasks/types.go`, `internal/api/handlers/analysis.go`):
   - Manual triggers deduplicated via `asynq.TaskID("analyze:manual:{repoID}")` with 10-minute retention
   - Returns 409 Conflict if analysis already pending/active
   - `TriggeredBy` field added to `AnalyzeRepoPayload` to track trigger source ("user" | "webhook")

2. ✅ **Token-Based Rate Limiting** (`internal/config/config.go`, `internal/storage/repository.go`, handlers):
   - Hourly token budget via `ANTHROPIC_TOKENS_PER_HOUR` (default 20,000)
   - Database SUM query checks accumulated tokens in last 60 minutes
   - Both manual triggers and webhooks respect limit
   - Returns 429 Too Many Requests when budget exhausted

3. ✅ **Local Code Metrics** (`internal/metrics/calculator.go`):
   - Uses go-git for shallow clone (Depth:1) with security hardening (no submodules)
   - Counts total lines, blank lines, code lines, and estimates cyclomatic complexity
   - Integrated into analysis worker — metrics computed before Claude call
   - Claude receives computed metrics in prompt with instruction not to recalculate
   - Uses `GITHUB_TOKEN` when configured and only sets clone auth when the token is non-empty
   - Graceful degradation: continues with zero metrics if clone fails

4. ✅ **Semantic Code Search** (`internal/embeddings/`, `internal/workers/embedding_worker.go`):
   - Voyage AI provider implementation using `voyage-code-3`
   - `TypeGenerateEmbeddings` worker with temporary git clone, deterministic chunking, batched embedding generation, and pgvector persistence
   - Protected endpoints for indexing and semantic search
   - Hybrid search combines vector similarity with textual boosts and `min_score` cutoff
   - Migration `007` updates `code_embeddings` to `VECTOR(1024)` and adds provider/model/dimension/branch metadata

**Ready for next phase**: Handler/integration test hardening and production key rotation
