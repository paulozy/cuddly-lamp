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

### Database & Migrations
- ✅ PostgreSQL 14+ with pgvector extension
- ✅ 4 SQL migrations (schema, auth, oauth_connections, refresh token rotation)
- ✅ Migration tracking via `schema_migrations` table (no re-runs on restart)
- ✅ Baseline detection for existing databases (safe upgrade path)
- ✅ OAuth connections table (provider + provider_user_id uniqueness)
- ✅ Soft deletes (deleted_at timestamps)
- ✅ Audit triggers (created_at, updated_at automation)

### API Routes
- ✅ Public routes: login, register, token refresh, OAuth (GitHub/GitLab)
- ✅ Protected routes: /users/me, logout
- ✅ Health check endpoint

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
  -d '{
    "email": "user@example.com",
    "full_name": "Test User",
    "password": "Password123"
  }'

# Get current user (requires JWT token from register/login)
curl -H "Authorization: Bearer <access_token>" \
  http://localhost:3000/api/v1/users/me

# Refresh access token using refresh token
curl -X POST http://localhost:3000/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token": "<refresh_token>"}'

# OAuth: redirect to GitHub (if configured)
curl -L http://localhost:3000/api/v1/auth/github
```

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
├── cmd/server/
│   └── main.go                    # Entry point (loads .env, starts HTTP server)
├── internal/
│   ├── api/
│   │   ├── handlers/
│   │   │   └── auth.go            # Login, register, OAuth, logout, /users/me
│   │   ├── middleware/
│   │   │   ├── auth.go            # JWT verification, context storage
│   │   │   ├── logger.go          # Request logging
│   │   │   ├── error_handler.go   # Global error handling
│   │   │   ├── optional_auth.go   # Optional auth (no 401 on missing token)
│   │   │   └── rbac.go            # Role-based access control
│   │   ├── factories/
│   │   │   └── make_auth_handler.go  # DI: auth service + providers
│   │   └── routes.go              # Route registration (/api/v1/*)
│   ├── oauth/                     # Multi-provider OAuth 2.0
│   │   ├── provider.go            # OAuthProvider interface
│   │   ├── github.go              # GitHub implementation
│   │   └── gitlab.go              # GitLab implementation
│   ├── services/
│   │   ├── auth_service.go        # JWT, password hashing (Argon2), OAuth, refresh tokens
│   │   └── auth_service_refresh_test.go  # Refresh token rotation tests
│   ├── models/
│   │   ├── user.go                # User with roles
│   │   ├── oauth_connection.go    # OAuth connections (provider links)
│   │   ├── token.go               # JWT token records
│   │   ├── repository.go          # Git repositories
│   │   ├── webhook.go             # Incoming webhooks
│   │   ├── code_analysis.go       # Code analysis results
│   │   ├── code_embedding.go      # Vector embeddings (pgvector)
│   │   └── auth.go                # Auth DTOs (LoginRequest, TokenResponse)
│   ├── config/
│   │   └── config.go              # Config struct + env loading
│   ├── storage/
│   │   ├── repository.go          # Repository interface
│   │   ├── postgres/
│   │   │   └── postgres_repository.go  # GORM implementation
│   │   ├── migrations.go          # SQL migration runner with schema_migrations tracking
│   │   └── storage.go             # Database initialization
│   └── utils/
│       ├── logger.go              # Structured logging (zap)
│       └── auth.go                # Token extraction, context helpers
├── migrations/
│   ├── 001-init-schema.sql        # Users, repos, webhooks, analysis, embeddings
│   ├── 002-add-auth-tables.sql    # Tokens, password_hash
│   ├── 003-add-oauth-connections.sql  # OAuth connections, migrate from users table
│   └── 004-add-refresh-token-rotation.sql  # family_id, parent_jti for token rotation
├── .env.example                   # Environment variables template
├── .env                           # Local env vars (git-ignored)
├── docker-compose.yml             # Dev: PostgreSQL + Redis
├── CLAUDE.md                      # Project guidelines & conventions
├── go.mod                         # Go module file
├── go.sum                         # Dependency hashes
├── Makefile                       # Commands: dev, build, test, lint
└── README.md                      # This file
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
- **REDIS_HOST, REDIS_PORT** - Redis
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

// Webhooks
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

### pgx/v5 Migration Quirk
- pgx/v5 does NOT support multiple SQL statements in `db.Exec()`
- Solution: Use underlying `*sql.DB` from `db.DB()` to run full migration files
- Migration runner uses `sqlDB.Exec(fileContent)` not `gorm.DB.Exec()`

### .env Loading
- Use `godotenv.Load()` in `main.go` before `config.Load()`
- Go does NOT load .env automatically

## 🎯 Next Steps

- [ ] Add repository management endpoints
- [ ] Implement webhook handlers (GitHub/GitLab)
- [ ] Code analysis API + Claude integration
- [ ] Semantic search with pgvector embeddings
- [ ] Rate limiting & request throttling
- [ ] Integration tests for migration runner (requires test DB)
- [ ] API documentation (Swagger/OpenAPI)

## 🤝 Contribuindo

Por favor, veja [CONTRIBUTING.md](docs/CONTRIBUTING.md) (a criar) para guidelines.

## 📄 License

MIT

## 📞 Contato

Para dúvidas ou sugestões, abra uma issue ou entre em contato com o time.

---

**Status**: 🔐 Auth Phase Complete (JWT + Refresh Tokens + OAuth + Migration Tracking)  
**Última atualização**: April 26, 2026