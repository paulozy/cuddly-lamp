# IDP Backend - Identity Provider with AI Integration

Identity Provider (IDP) platform with JWT authentication, multi-provider OAuth 2.0 (GitHub, GitLab), and semantic code search integration. Built with Go, PostgreSQL, and pgvector for embeddings.

## ✨ Features Implemented

### Authentication & Authorization
- ✅ Email/Password registration & login (Argon2 hashing)
- ✅ JWT access tokens with revocation tracking (JTI per token)
- ✅ Refresh token rotation (RFC 6749 §6 / RFC 9700 compliant)
- ✅ Refresh token reuse detection with family revocation (anti-hijacking)
- ✅ OAuth 2.0 Authorization Code Flow (GitHub, GitLab infrastructure ready)
- ✅ Stateless HMAC-signed CSRF state tokens
- ✅ Account linking (OAuth to existing email users)
- ✅ Role-based access control (admin, maintainer, developer, viewer)
- ✅ Token logout (access + full refresh family revocation)

### Encryption & Data Security
- ✅ AES-256-GCM encryption for sensitive fields (OAuth tokens, webhook secrets)
- ✅ Key rotation support (base64-encoded 32-byte key via `ENCRYPTION_KEY`)
- ✅ Transparent field-level encryption via GORM hooks
- ✅ CLI migration tool for encrypting existing unencrypted data (`cmd/migrate-encrypt/`)
- ✅ Automatic encryption on save, decryption on load

### Database & Migrations
- ✅ PostgreSQL 14+ with pgvector extension
- ✅ 6 SQL migrations (schema, auth, oauth_connections, refresh token rotation, encryption fields)
- ✅ Migration tracking via `schema_migrations` table (no re-runs on restart)
- ✅ Baseline detection for existing databases (safe upgrade path)
- ✅ OAuth connections table (provider + provider_user_id uniqueness)
- ✅ Soft deletes (deleted_at timestamps)
- ✅ Audit triggers (created_at, updated_at automation)
- ✅ Encrypted fields: OAuth tokens (access_token_encrypted), webhook secrets (secret_encrypted)

### Repository Management
- ✅ CRUD endpoints — create, list, get, update, delete repositories
- ✅ GitHub sync — fetches metadata (branches, commits, PRs, languages, stars, forks)
- ✅ Sync status lifecycle — `idle → syncing → synced / error`
- ✅ WebhookConfig — registers GitHub webhook on sync, stores HMAC secret
- ✅ Webhook registration skipped automatically on localhost (use ngrok for local dev)

### Webhook Pipeline
- ✅ HMAC-SHA256 signature validation (X-Hub-Signature-256)
- ✅ Idempotency via `X-GitHub-Delivery` ID — duplicate deliveries return 200
- ✅ Events persisted to `webhooks` table with status tracking and retry logic
- ✅ Background processing worker (`webhook:process` asynq task)

### API Routes
- ✅ Public routes: login, register, token refresh, OAuth (GitHub/GitLab)
- ✅ Public webhook receiver: `POST /api/v1/webhooks/github/:repoID` (HMAC auth)
- ✅ Protected routes: /users/me, logout
- ✅ Protected repository routes: CRUD on `/api/v1/repositories`
- ✅ Health check endpoint

### Infrastructure
- ✅ Redis client (go-redis/v9) with connection pool and graceful no-op fallback
- ✅ Cache layer — `Cache` interface with `ErrCacheMiss`, centralised key builders (`TokenKey`, `UserKey`, `RepoKey`)
- ✅ Job queue — `Enqueuer` interface backed by `asynq` (retries, cron, dead-letter, priority queues)
- ✅ Background workers — `SyncWorker` (repo:sync) + `WebhookProcessor` (webhook:process), graceful shutdown
- ✅ GitHub API client — `internal/integrations/github/` (repos, branches, commits, PRs, webhooks)
- ✅ Server boots without Redis — cache + queue degrade silently to no-op

### API Documentation
- ✅ Swagger/OpenAPI 2.0 with swaggo/swag
- ✅ Interactive Swagger UI at `/swagger/index.html`
- ✅ Comprehensive annotations on all 13 endpoints (auth, repository, webhook)
- ✅ JWT security scheme documented
- ✅ Automatic generation with `make swagger`

### Code Quality
- ✅ Structured logging (zap)
- ✅ .env file loading (godotenv)
- ✅ Error handling & CORS middleware
- ✅ API versioning (/api/v1)
- ✅ CLAUDE.md project guide

## 📋 Prerequisites

- Go 1.21+
- Docker & Docker Compose
- Git

## 🚀 Quick Start

### 1. Setup environment
```bash
cp .env.example .env
# Fill in GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET, GITHUB_CALLBACK_URL
```

### 2. Start services (PostgreSQL, Redis)
```bash
make docker-up
```

### 3. Run server
```bash
make dev
```

The server will:
- Load `.env` variables
- Run pending migrations (skips already applied ones)
- Register OAuth providers (GitHub if configured)
- Start HTTP server on port 3000

### 4. Test the server
```bash
# Health check
curl http://localhost:3000/api/v1/health

# Register with email/password
curl -X POST http://localhost:3000/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "full_name": "Test User", "password": "Password123"}'

# Login and capture token
TOKEN=$(curl -s -X POST http://localhost:3000/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"Password123"}' | jq -r '.access_token')

# Add a repository (triggers GitHub sync automatically)
curl -X POST http://localhost:3000/api/v1/repositories \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://github.com/owner/repo"}'

# List repositories
curl -H "Authorization: Bearer $TOKEN" http://localhost:3000/api/v1/repositories

# OAuth: redirect to GitHub (if configured)
curl -L http://localhost:3000/api/v1/auth/github
```

> For webhook testing with ngrok see [`tests/GITHUB_SYNC_TESTING.md`](tests/GITHUB_SYNC_TESTING.md).

## 📚 Documentação

- **[SETUP.md](docs/SETUP.md)** - Setup detalhado (banco, ambiente, etc)
- **[MIGRATIONS.md](docs/MIGRATIONS.md)** - Como criar e gerenciar migrations
- **[API.md](docs/API.md)** - Documentação de endpoints (em progresso)
- **[ARCHITECTURE.md](docs/ARCHITECTURE.md)** - Visão geral da arquitetura (em progresso)

## 🛠️ Comandos Úteis

### Development
```bash
make dev              # Inicia servidor em modo desenvolvimento
make build            # Compila binário
make run              # Executa binário compilado
```

### Testing
```bash
make test             # Roda testes
make test-coverage    # Testes com coverage report
make lint             # Roda linter
```

### Docker
```bash
make docker-up        # Inicia PostgreSQL + Redis
make docker-down      # Para os serviços
make docker-logs      # Mostra logs dos containers
```

### Utilities
```bash
make fmt              # Formata código (gofmt)
make mod-tidy         # Atualiza go.mod/go.sum
make clean            # Remove build artifacts
make swagger          # Gera documentação Swagger/OpenAPI
```

## 🔐 Setting Up GitHub OAuth

1. Create GitHub OAuth App:
   - Go to https://github.com/settings/developers → OAuth Apps → New OAuth App
   - Application name: `IDP Backend Local`
   - Homepage URL: `http://localhost:3000`
   - Authorization callback URL: `http://localhost:3000/api/v1/auth/github/callback`

2. Copy Client ID and Client Secret

3. Add to `.env`:
   ```bash
   GITHUB_CLIENT_ID=<your-client-id>
   GITHUB_CLIENT_SECRET=<your-client-secret>
   GITHUB_CALLBACK_URL=http://localhost:3000/api/v1/auth/github/callback
   ```

4. Restart server (`make dev`)

5. Test OAuth:
   ```bash
   # User clicks: http://localhost:3000/api/v1/auth/github
   # Redirects to GitHub login
   # GitHub redirects back to callback with token
   # Returns: TokenResponse with JWT and user info
   ```

## 📁 Project Structure

```
backend/
├── cmd/
│   ├── server/
│   │   └── main.go                    # Entry point — wires DB, Redis, workers, HTTP server
│   └── migrate-encrypt/
│       └── main.go                    # CLI tool to encrypt existing sensitive fields
├── internal/
│   ├── api/
│   │   ├── handlers/
│   │   │   ├── auth.go            # Login, register, OAuth, logout, /users/me
│   │   │   ├── repository.go      # Repository CRUD endpoints
│   │   │   └── webhook.go         # GitHub webhook receiver (HMAC validation)
│   │   ├── middleware/
│   │   │   ├── auth.go            # JWT verification, context storage
│   │   │   ├── logger.go          # Request logging
│   │   │   ├── error_handler.go   # Global error handling
│   │   │   ├── optional_auth.go   # Optional auth (no 401 on missing token)
│   │   │   └── rbac.go            # Role-based access control
│   │   ├── factories/
│   │   │   ├── make_auth_handler.go        # DI: auth service + providers
│   │   │   ├── make_repository_handler.go  # DI: repository service
│   │   │   └── make_webhook_handler.go     # DI: webhook handler
│   │   └── routes.go              # Route registration (/api/v1/*)
│   ├── crypto/                    # Field-level encryption (AES-256-GCM)
│   │   ├── cipher.go              # Encrypt/decrypt functions
│   │   ├── cipher_test.go         # Cipher tests
│   │   └── serializer.go          # GORM hooks for transparent encryption
│   ├── integrations/
│   │   └── github/                # GitHub API client (repos, branches, commits, PRs, webhooks)
│   │       ├── client.go          # HTTP client + ClientInterface
│   │       └── validation.go      # HMAC-SHA256 webhook signature validation
│   ├── oauth/                     # Multi-provider OAuth 2.0
│   │   ├── provider.go            # OAuthProvider interface
│   │   ├── github.go              # GitHub implementation
│   │   └── gitlab.go              # GitLab implementation
│   ├── services/
│   │   ├── auth_service.go        # JWT, password hashing (Argon2), OAuth, refresh tokens
│   │   ├── repository_service.go  # Repository business logic (ownership, dedup)
│   │   ├── sync_service.go        # GitHub sync (metadata + webhook registration)
│   │   └── *_test.go              # Unit tests (auth refresh, repository, sync)
│   ├── workers/
│   │   ├── sync_worker.go         # Handles repo:sync asynq task
│   │   └── webhook_processor.go   # Handles webhook:process asynq task
│   ├── models/
│   │   ├── user.go                # User with roles
│   │   ├── oauth_connection.go    # OAuth connections (provider links)
│   │   ├── auth.go                # Auth DTOs (LoginRequest, TokenResponse)
│   │   ├── repository.go          # Repository + RepositoryMetadata + StringArray
│   │   ├── repository_dto.go      # CreateRepositoryRequest, UpdateRepositoryRequest
│   │   ├── webhook.go             # Webhook events + WebhookConfig + StringArray type
│   │   ├── code_analysis.go       # Code analysis results
│   │   └── code_embedding.go      # Vector embeddings (pgvector)
│   ├── config/
│   │   └── config.go              # Config struct + env loading (incl. WEBHOOK_BASE_URL)
│   ├── storage/
│   │   ├── repository.go          # Repository interface
│   │   ├── postgres/
│   │   │   └── postgres_repository.go  # GORM implementation
│   │   ├── redis/
│   │   │   ├── redis.go           # RedisClient interface + impl + no-op
│   │   │   ├── cache.go           # Cache interface (Get/Set/Del/Exists + ErrCacheMiss)
│   │   │   └── keys.go            # Key builders (TokenKey, UserKey, RepoKey, ...)
│   │   ├── migrations.go          # SQL migration runner with schema_migrations tracking
│   │   └── storage.go             # Database initialization + GORM logger config
│   ├── jobs/
│   │   ├── queue.go               # Enqueuer interface + asynq impl + no-op
│   │   ├── worker.go              # asynq worker server (priority queues, graceful shutdown)
│   │   └── tasks/
│   │       └── types.go           # Task type constants + payload structs
│   └── utils/
│       ├── logger.go              # Structured logging (zap)
│       ├── auth.go                # Token extraction, context helpers
│       └── repository.go          # URL parsing helpers (ParseRepositoryURL)
├── migrations/
│   ├── 001-init-schema.sql        # Users, repos, webhooks, analysis, embeddings
│   ├── 002-add-auth-tables.sql    # Tokens, password_hash
│   ├── 003-add-oauth-connections.sql  # OAuth connections, migrate from users table
│   ├── 004-add-refresh-token-rotation.sql  # family_id, parent_jti for token rotation
│   ├── 005-add-synced-status.sql  # Add 'synced' to sync_status check constraint
│   └── 006-encrypt-sensitive-fields.sql  # Add encrypted columns (access_token_encrypted, secret_encrypted)
├── tests/
│   └── GITHUB_SYNC_TESTING.md     # Manual integration testing guide (sync + webhooks)
├── .env.example                   # Environment variables template
├── docker-compose.yml             # Dev: PostgreSQL + Redis
├── CLAUDE.md                      # Project guidelines & conventions
├── go.mod / go.sum
├── Makefile
└── README.md
```

## 🗄️ Database

### Conectar ao PostgreSQL
```bash
docker-compose exec postgres psql -U postgres -d idp_dev
```

### Ver tabelas criadas
```sql
\dt
```

### Ver estrutura de uma tabela
```sql
\d repositories
```

### Conectar ao Redis
```bash
docker-compose exec redis redis-cli
```

## 🔒 Variáveis de Ambiente

Veja `.env.example` para todas as variáveis disponíveis:

- **PORT** - Porta do servidor (default: 3000)
- **ENV** - Ambiente (development/production)
- **DB_HOST, DB_USER, DB_PASSWORD, DB_NAME** - PostgreSQL
- **REDIS_HOST, REDIS_PORT, REDIS_PASSWORD, REDIS_DB** - Redis (optional — app starts without it)
- **JWT_SECRET** - Secret for JWT signing and state token validation
- **ENCRYPTION_KEY** - Base64-encoded 32-byte key for AES-256-GCM encryption (generate with `openssl rand -base64 32`)
- **ANTHROPIC_API_KEY** - Claude API key
- **GITHUB_TOKEN** - GitHub access token
- **LOG_LEVEL** - Nível de logging (debug/info/warn/error)

## 🚨 Troubleshooting

### PostgreSQL não conecta
```bash
# Verificar se containers estão rodando
docker-compose ps

# Se não, iniciar
make docker-up

# Se der erro, limpar e recomeçar
docker-compose down -v  # Remove volumes
docker-compose up -d
```

### Porta 8080 em uso
```bash
# Mudar porta em .env
PORT=3000
```

### Migrations falharam
```bash
# Ver logs do PostgreSQL
docker-compose logs postgres

# Conectar e verificar tabelas e migrations aplicadas
docker-compose exec postgres psql -U postgres -d idp_dev -c "\dt"
docker-compose exec postgres psql -U postgres -d idp_dev -c "SELECT * FROM schema_migrations ORDER BY applied_at;"
```

## 📊 Models & Database

### Models implementados
- **User** - Usuários com OAuth (GitHub, GitLab)
- **Repository** - Repositórios com tracking de análises
- **Webhook** - Webhooks com retry logic e status de processamento
- **CodeAnalysis** - Análises de código com issues, métricas e embeddings
- **CodeEmbedding** - Embeddings vetoriais para busca semântica (pgvector)

### Database
- **8 tabelas principais** com indexes otimizados
- **JSONB** para dados flexíveis (metadata, issues, métricas)
- **pgvector** para semantic search
- **Soft deletes** (deleted_at column)
- **Triggers** para audit (updated_at automático)
- **Cascading deletes** para integridade referencial

### Repository Operations
Implementadas operações CRUD para todas as entidades:
```go
// Users
GetUser, GetUserByEmail, GetUserByGitHubID, CreateUser, UpdateUser, ListUsers

// Repositories
GetRepository, GetRepositoryByURL, CreateRepository, UpdateRepository,
ListRepositories, DeleteRepository, SearchRepositories

// WebhookConfigs
GetWebhookConfigByRepoID, CreateWebhookConfig, UpdateWebhookConfig

// Webhooks (events)
GetWebhook, GetWebhookByDeliveryID, CreateWebhook, UpdateWebhook,
ListPendingWebhooks, ListFailedWebhooks

// Code Analysis
GetCodeAnalysis, CreateCodeAnalysis, UpdateCodeAnalysis, ListAnalyses,
GetLatestAnalysis, GetRepositoriesNeedingAnalysis

// Code Embeddings
CreateCodeEmbedding, SearchEmbeddings, DeleteEmbeddingsByRepository
```

## ⚙️ Important Implementation Details

### Timezone Handling
- **Always use UTC**: `time.Now().UTC()` before storing timestamps
- PostgreSQL `TIMESTAMP` columns have no timezone — explicit UTC prevents offset bugs
- Validation compares both sides in UTC: `time.Now().UTC().After(record.ExpiresAt.UTC())`

### Password Hashing (Argon2)
```
Argon2 IDKey: 2 time iterations, 64MB memory, 4 parallelism, 32-byte hash
16-byte random salt per password (no global boost secret)
Format: <hex-salt>$<hex-hash>
```

### OAuth State (CSRF Protection)
```
Stateless signed state token (no Redis needed):
- Payload: base64url(json{"nonce":"<random>","exp":<unix>})
- Signature: base64url(HMAC-SHA256(payload, jwtSecret))
- Format: <payload>.<signature>
- Expiry: 10 minutes
```

### Refresh Token Security (RFC 9700)
```
Token flow:
  login/register → { access_token (JWT, 15min), refresh_token (opaque, 7d) }
  POST /auth/refresh → consumes old refresh token, issues new pair (rotation)
  Reuse detection: replayed token → entire family revoked (anti-hijacking)

Storage:
  Refresh tokens stored as SHA-256(raw) — never cleartext
  family_id links all rotations of the same session
  parent_jti traces the rotation chain
```

### Migration Tracking
- `schema_migrations` table records each applied filename + timestamp
- Runner skips files already in the table — safe to restart at any time
- Baseline mode: if `users` exists but `schema_migrations` is empty, all current files are seeded as applied (handles upgrades from pre-tracking deployments)

### Redis & Job Queue
```
Cache layer (internal/storage/redis):
  Cache interface — Get/Set/Del/Exists with ErrCacheMiss sentinel
  Key builders — TokenKey(jti), UserKey(id), SessionKey(id)
  No-op fallback — NewNoop() / NewNoopCache() used when Redis is offline

Job queue (internal/jobs):
  Enqueuer interface — Enqueue / EnqueueIn with asynq.Option pass-through
  asynq backend — retries, scheduling, dead-letter, asynqmon UI
  Priority queues: critical (weight 6) > default (3) > low (1)
  Worker runs in-process; register handlers with worker.Register(taskType, fn)
  No-op fallback — NewNoopEnqueuer() logs and discards jobs silently

Task type constants (internal/jobs/tasks):
  TypeSyncRepo, TypeAnalyzeRepo, TypeProcessWebhook, TypeGenerateEmbeddings

Key builders (internal/storage/redis/keys.go):
  TokenKey(jti), UserKey(id), RepoKey(id), SessionKey(id)
```

### pgx/v5 Migration Quirk
- pgx/v5 does NOT support multiple SQL statements in `db.Exec()`
- Solution: Use underlying `*sql.DB` from `db.DB()` to run full migration files
- Migration runner uses `sqlDB.Exec(fileContent)` not `gorm.DB.Exec()`

### .env Loading
- Use `godotenv.Load()` in `main.go` before `config.Load()`
- Go does NOT load .env automatically

## 🎯 Next Steps

- [x] Repository management endpoints (CRUD + GitHub sync)
- [x] Webhook pipeline (GitHub HMAC ingestion + background processing)
- [x] Encryption for sensitive fields (OAuth tokens, webhook secrets)
- [x] API documentation (Swagger/OpenAPI)
- [ ] Code analysis API + Claude integration — wire `TypeAnalyzeRepo` job
- [ ] Semantic search with pgvector embeddings — wire `TypeGenerateEmbeddings` job
- [ ] Rate limiting & request throttling
- [ ] Integration tests for handlers and postgres repository (requires test DB)

## 🤝 Contribuindo

Por favor, veja [CONTRIBUTING.md](docs/CONTRIBUTING.md) (a criar) para guidelines.

## 📄 License

MIT

## 📞 Contato

Para dúvidas ou sugestões, abra uma issue ou entre em contato com o time.

---

**Status**: 📚 Swagger/OpenAPI Complete (Auth + Sync + Webhook + Encryption + Docs)  
**Última atualização**: April 28, 2026

### 📖 Accessing the API Documentation
```bash
make dev
# Open browser: http://localhost:3000/swagger/index.html
```