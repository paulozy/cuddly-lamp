# Project Checkpoint - April 28, 2026

## 📌 What Has Been Implemented

### Authentication System ✅
- Email/Password registration with Argon2 hashing (2 iterations, 64MB, 4 parallelism)
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
- **Coverage**: All 13 endpoints documented (7 auth, 5 repository, 1 webhook)
- **Annotations**: Complete with `@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Security` markers
- **Security**: JWT BearerAuth scheme documented; webhook HMAC headers documented
- **Generation**: `make swagger` rebuilds docs/ from annotations
- **Files**: docs/docs.go committed (for consumers without swag CLI), docs/swagger.json/yaml ignored (.gitignore)

### Infrastructure ✅
- Redis cache layer — `Cache` interface with `ErrCacheMiss`, no-op fallback
- Key builders: `TokenKey`, `UserKey`, `RepoKey`, `SessionKey`
- asynq job queue — `Enqueuer` interface, priority queues (critical/default/low), dead-letter
- Background workers registered in-process: `SyncWorker` (`repo:sync`) + `WebhookProcessor` (`webhook:process`)
- GitHub API client (`internal/integrations/github/`) — repos, branches, commits, PRs, webhooks
- GORM logger configured: `IgnoreRecordNotFoundError: true`, 200ms slow query threshold
- Server boots without Redis — cache and queue degrade silently to no-op

### Database & Migrations ✅
- 6 SQL migrations applied and tracked via `schema_migrations`
  - `001`: Initial schema — 8 tables + triggers + pgvector
  - `002`: Auth tables — tokens, password_hash
  - `003`: OAuth connections — provider uniqueness, data migration from users
  - `004`: Refresh token rotation — `family_id`, `parent_jti` columns on tokens
  - `005`: Sync status — added `'synced'` to `repositories.sync_status` check constraint
  - `006`: Encrypted fields — `access_token_encrypted` (bytea) on `oauth_connections`, `secret_encrypted` (bytea) on `webhook_configs`
- `StringArray` custom type for PostgreSQL `text[]` columns (implements `driver.Valuer` + `sql.Scanner`)
- Baseline detection for databases pre-dating migration tracking

---

## 📡 API Endpoints

**Public Routes** (`/api/v1`):
| Method | Path | Description |
|--------|------|-------------|
| POST | `/auth/register` | Email/password registration → JWT pair |
| POST | `/auth/login` | Email/password login → JWT pair |
| POST | `/auth/refresh` | Rotate refresh token → new JWT pair |
| GET | `/auth/:provider` | OAuth redirect (github, gitlab) |
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

---

## 🗄️ Database Schema

| Table | Purpose |
|-------|---------|
| `users` | Platform users with roles, soft deletes |
| `oauth_connections` | OAuth provider links (provider + provider_user_id unique) |
| `tokens` | JWT records with revocation, family_id, parent_jti |
| `repositories` | Git repos with sync_status, metadata (JSONB), analysis_status |
| `webhook_configs` | Per-repo webhook registrations with HMAC secret |
| `webhooks` | Incoming webhook events with status, retry, idempotency |
| `code_analyses` | Code review results (pending use) |
| `code_embeddings` | pgvector embeddings for semantic search (pending use) |
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

---

## 📊 Test Coverage

| Package | Coverage | Notes |
|---------|----------|-------|
| `internal/services` | Unit tests ✅ | auth refresh, repository CRUD, sync pipeline |
| `internal/storage/redis` | Unit tests ✅ | cache get/set/del/exists, no-op fallback |
| `internal/utils` | Unit tests ✅ | URL parsing |
| `internal/storage/postgres` | Integration tests ⏳ | requires `TEST_DATABASE_URL` |
| `internal/api/handlers` | None ❌ | next priority |

---

## 🎯 Next Steps

- [ ] **AI Integration** — Claude API for code analysis; wire `TypeAnalyzeRepo` job
- [ ] **Semantic search** — pgvector embeddings; wire `TypeGenerateEmbeddings` job
- [ ] **Handler tests** — unit tests for repository and webhook handlers
- [ ] **Postgres integration tests** — wire `TEST_DATABASE_URL` in CI
- [ ] **Rate limiting** — per-user request throttling
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
GITHUB_TOKEN                 # Personal access token (repo + admin:repo_hook scopes)
GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET, GITHUB_CALLBACK_URL

# Webhooks
WEBHOOK_BASE_URL             # Public URL for webhook registration (ngrok in local dev)
                             # Leave empty or use localhost to skip GitHub registration

# API
ANTHROPIC_API_KEY            # Claude API (pending use)
LOG_LEVEL                    # debug / info / warn / error
```

---

**Status**: 📚 Swagger/OpenAPI Complete (Auth + Repo + Webhook + Encryption + Docs)  
**Commits this phase**: 3 (encryption feature + docs checkpoint + swagger feature)  
**Production Readiness**: ~70% (auth + repo + webhook + encryption + docs done; needs AI integration, tests, rate limiting)

---

## 📖 API Documentation Access

**Interactive Swagger UI:**
```bash
make dev
# Open: http://localhost:3000/swagger/index.html
```

**13 documented endpoints:**
- 7 Auth endpoints (login, register, refresh, OAuth, logout, /users/me)
- 5 Repository endpoints (CRUD)
- 1 Webhook endpoint (GitHub receiver)

**Features:**
- ✅ JWT security scheme (BearerAuth)
- ✅ Try-it-out functionality (test endpoints from UI)
- ✅ Request/response examples
- ✅ Error codes documented
