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
- **AI Integration**: Anthropic API (Claude for code analysis)

## Architecture

```
internal/
├── api/
│   ├── handlers/         # HTTP request handlers (auth, repository, webhook)
│   ├── middleware/       # JWT auth, CORS, logging
│   ├── routes.go         # Route definitions
│   └── factories/        # Dependency injection setup
├── integrations/
│   └── github/           # GitHub API client + webhook HMAC validation
├── models/               # GORM models (User, Repository, Token, WebhookConfig, etc.)
├── services/             # Business logic (AuthService, RepositoryService, SyncService)
├── workers/              # asynq task handlers (SyncWorker, WebhookProcessor)
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

1. **Authentication & JWT**: Complete — email/password, JWT access tokens, refresh token rotation (RFC 9700), OAuth (GitHub/GitLab)
2. **Infrastructure**: Complete — Redis cache layer + asynq job queue wired in `main.go` with no-op fallbacks
3. **Repository management**: Complete — CRUD endpoints, GitHub sync (branches, commits, PRs, languages), WebhookConfig registration
4. **Webhook pipeline**: Complete — HMAC-validated ingestion, idempotency via delivery ID, background processing worker
5. **Field-level encryption**: Complete — AES-256-GCM encryption for sensitive fields (OAuth tokens, webhook secrets), transparent GORM hooks, CLI migration tool
6. **Next: AI Integration** — Claude integration for code analysis (`TypeAnalyzeRepo` job); wire `TypeGenerateEmbeddings` for pgvector search

## Known Issues & Constraints

- **Partial test coverage**: services and redis have tests; postgres integration tests and handlers have none yet
- **Token validation**: Allows tokens not found in DB for backward compatibility (tokens created before DB migration)
- **Soft deletes**: Using `*time.Time` for DeletedAt (nullable), not `gorm.DeletedAt`
- **Worker in-process**: asynq worker runs in the same binary as the HTTP server; split to `cmd/worker/` when independent scaling is needed
- **Timezone handling**: PostgreSQL TIMESTAMP (no timezone) requires explicit UTC conversion in Go — always use `.UTC()`
- **StringArray**: `models.StringArray` is a custom type for PostgreSQL `text[]` — use it instead of `[]string` on any GORM model field mapped to a `text[]` column
- **Webhook registration on localhost**: skipped automatically when `WEBHOOK_BASE_URL` contains `localhost`/`127.0.0.1` — use ngrok for local webhook testing
- **Field-level encryption**: Encrypted fields require `ENCRYPTION_KEY` at startup; existing unencrypted data must be migrated using `cmd/migrate-encrypt/` tool; decryption happens transparently via GORM `AfterFind` hooks

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

## Environment Configuration

`.env` variables (see `.env.example`):
- `DB_*`: PostgreSQL connection
- `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`, `REDIS_DB`: Redis (optional — app starts without it)
- `JWT_SECRET`, `JWT_ISSUER`, `JWT_AUDIENCE`: JWT configuration
- `ACCESS_TOKEN_TTL`, `REFRESH_TOKEN_TTL`: Token expiration (in minutes)
- `ENCRYPTION_KEY`: Base64-encoded 32-byte AES-256-GCM key for field encryption (generate with `openssl rand -base64 32`)
- `ANTHROPIC_API_KEY`, `GITHUB_TOKEN`: External API keys
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
