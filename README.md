# IDP Backend - Identity Provider with AI Integration

Identity Provider (IDP) platform with JWT authentication, multi-provider OAuth 2.0 (GitHub, GitLab), AI code/dependency analysis, auto-generated repository documentation, intelligent code templates, and semantic code search integration. Built with Go, PostgreSQL, and pgvector for embeddings.

## ✨ Features Implemented

### Authentication & Authorization
- ✅ Email/Password registration & login (Argon2 hashing)
- ✅ Organization onboarding during registration (`organization_name`, optional derived slug)
- ✅ Multi-organization login flow with short-lived selection tickets
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
- ✅ 11 SQL migrations (schema, auth, oauth_connections, refresh token rotation, encryption fields, embeddings, multitenancy, package dependencies, code templates, doc generations)
- ✅ Migration tracking via `schema_migrations` table (no re-runs on restart)
- ✅ Baseline detection for existing databases (safe upgrade path)
- ✅ OAuth connections table (provider + provider_user_id uniqueness)
- ✅ Soft deletes (deleted_at timestamps)
- ✅ Audit triggers (created_at, updated_at automation)
- ✅ Encrypted fields: OAuth tokens (access_token_encrypted), webhook secrets (secret_encrypted)
- ✅ Package dependency inventory with CVE/update metadata
- ✅ Code template storage with generated files as JSONB and pinning metadata
- ✅ Doc generation storage with generated Markdown content as JSONB and PR metadata

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
- ✅ Public routes: login, organization selection, register, token refresh, OAuth (GitHub/GitLab)
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
- ✅ Comprehensive annotations for auth, repository, webhook, analysis, dependency, template, semantic search, and health endpoints
- ✅ Documentation generation endpoint annotation
- ✅ JWT security scheme documented
- ✅ Automatic generation with `make swagger`

### AI Integration
- ✅ Pluggable `ai.Analyzer` interface for code analysis
- ✅ Pluggable `ai.DocumentationGenerator` interface for generated repository documentation
- ✅ Pluggable `ai.Generator` interface for code template generation
- ✅ Anthropic (Claude) implementation with Anthropic SDK
- ✅ Analysis worker (`TypeAnalyzeRepo` job) — triggers on push/PR webhook events
- ✅ Pull request analysis with real GitHub PR metadata/files, diff filtering, token budgeting, and file-level commenting
- ✅ PR review posting (optional via `GITHUB_PR_REVIEW_ENABLED=true`)
- ✅ HTTP endpoints: `POST /repositories/:id/analyze`, `GET /repositories/:id/analyses`
- ✅ Support for multiple analysis types: `code_review`, `security`, `architecture`
- ✅ Doc-aware analysis: latest generated docs are injected into analysis prompts as `PROJECT STANDARDS / DOCUMENTATION`
- ✅ Auto-trigger: analyze repositories on `push` events, create PR comments on `pull_request` events
- ✅ Deduplication: manual trigger deduplication via asynq.TaskID (returns 409 on conflict)
- ✅ Token rate limiting: hourly budget (default 20K tokens/hour, configurable)
- ✅ Local metrics: code complexity and line counting via shallow git clone before AI analysis, using `GITHUB_TOKEN` for private repositories when configured
- ✅ Future-proof architecture: swap providers (Claude → OpenAI, etc.) with one-line change

### Auto-Generated Documentation
- ✅ HTTP endpoint: `POST /repositories/:id/docs/generate`
- ✅ Supported doc types: `adr`, `architecture`, `service_doc`, `guidelines`
- ✅ Async flow: create `doc_generations` row → enqueue `docs:generate` → clone repo → generate Markdown → commit files → open GitHub PR
- ✅ GitHub Contents API integration for branch creation and create/update file commits
- ✅ Generated files: `docs/adr/README.md`, `docs/ARCHITECTURE.md`, `docs/SERVICE.md`, `CONTRIBUTING.md`
- ✅ Generated Markdown stored in PostgreSQL JSONB for fast cross-reference during future code analysis
- ✅ Shared Anthropic token budget check on manual trigger

### Intelligent Code Templates
- ✅ AI-powered code scaffold generation via Claude (`ai.Generator`)
- ✅ Repository-scoped generation using detected stack metadata (`languages`, `frameworks`, `topics`, CI/tests)
- ✅ Organization-level generation with optional `stack_hint`
- ✅ Async flow: create `code_templates` row → enqueue `template:generate` → poll result
- ✅ Generated multi-file output stored inline as JSONB (`files[]` with path/content/language)
- ✅ Pin/unpin templates for team reuse with optional display name
- ✅ HTTP endpoints: `POST /templates`, `POST /repositories/:id/templates`, `GET /templates/:id`, `GET /templates`, `PATCH /templates/:id/pin`
- ✅ Shared Anthropic token budget with code analysis and dependency analysis
- ✅ Swagger annotations and unit tests for generator parsing and worker transitions

### Dependency Tracking
- ✅ Manifest parsers for `go.mod`, `package.json`, `requirements.txt`, and `Cargo.toml`
- ✅ Package inventory stored in `package_dependencies` with unique `(repository_id, name, ecosystem)` upserts
- ✅ Dependency scan worker (`dependency:scan`) — shallow clone, parse manifests, call Claude, persist analysis and vulnerability status
- ✅ Claude dependency analysis for CVEs, outdated packages, license risks, transitive risks, and recommended versions
- ✅ HTTP endpoints: `POST /repositories/:id/dependencies/scan`, `GET /repositories/:id/dependencies?vulnerable=true`
- ✅ Webhook auto-trigger when push/PR changes include supported manifest files
- ✅ Suggestion-based updates only: recommended versions are stored/commented, no automatic update PR creation

### Semantic Code Search
- ✅ Voyage AI embeddings with provider abstraction (`internal/embeddings`)
- ✅ Default model: `voyage-code-3` with 1024-dimensional vectors
- ✅ `embeddings:generate` worker — temporary repository clone, source-code chunking, batched embedding generation
- ✅ Hybrid semantic search: pgvector cosine ranking plus textual boosts for content, file path, and language matches
- ✅ Relevance cutoff via `min_score` so weak searches can return zero results instead of noisy matches
- ✅ HTTP endpoints: `POST /repositories/:id/embeddings`, `GET /repositories/:id/search?q=...&min_score=0.55`
- ✅ Provider/model/dimension/branch metadata persisted for future provider swaps

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

### 4. Generate and query semantic embeddings
```bash
# Requires VOYAGE_API_KEY and Redis/asynq enabled
curl -X POST http://localhost:3000/api/v1/repositories/$REPO_ID/embeddings \
  -H "Authorization: Bearer $TOKEN"

curl "http://localhost:3000/api/v1/repositories/$REPO_ID/search?q=where%20is%20token%20rotation%20handled&limit=10&min_score=0.55" \
  -H "Authorization: Bearer $TOKEN"
```

### 5. Scan repository dependencies
```bash
# Requires ANTHROPIC_API_KEY, Redis/asynq enabled, and GITHUB_TOKEN for private repositories
curl -X POST http://localhost:3000/api/v1/repositories/$REPO_ID/dependencies/scan \
  -H "Authorization: Bearer $TOKEN"

curl "http://localhost:3000/api/v1/repositories/$REPO_ID/dependencies?vulnerable=true" \
  -H "Authorization: Bearer $TOKEN"
```

### 6. Test the server
```bash
# Health check
curl http://localhost:3000/api/v1/health

# Register with email/password
curl -X POST http://localhost:3000/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "full_name": "Test User", "password": "Password123", "organization_name": "Acme Inc"}'

# Login and capture token
# If the user belongs to multiple organizations, the response includes
# requires_organization_selection=true, login_ticket, and organizations[].
TOKEN=$(curl -s -X POST http://localhost:3000/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"Password123"}' | jq -r '.access_token')

# Complete multi-organization login when required
curl -X POST http://localhost:3000/api/v1/auth/select-organization \
  -H "Content-Type: application/json" \
  -d '{"login_ticket":"LOGIN_TICKET","organization_id":"ORG_ID"}'

# Add a repository (triggers GitHub sync automatically)
curl -X POST http://localhost:3000/api/v1/repositories \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://github.com/owner/repo"}'

# List repositories
curl -H "Authorization: Bearer $TOKEN" http://localhost:3000/api/v1/repositories

# OAuth onboarding: redirect to GitHub (if configured)
curl -L "http://localhost:3000/api/v1/auth/github?organization_name=Acme%20Inc"
```

### 7. Generate an intelligent code template
```bash
# Requires ANTHROPIC_API_KEY and Redis/asynq enabled
curl -X POST http://localhost:3000/api/v1/repositories/$REPO_ID/templates \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"Create a CRUD API with JWT auth","stack_hint":"Go, Gin, GORM"}'

curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:3000/api/v1/templates/$TEMPLATE_ID

curl -X PATCH http://localhost:3000/api/v1/templates/$TEMPLATE_ID/pin \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"is_pinned":true,"name":"Go CRUD with JWT"}'
```

### 8. Generate repository documentation
```bash
# Requires ANTHROPIC_API_KEY, GITHUB_TOKEN, and Redis/asynq enabled
curl -X POST http://localhost:3000/api/v1/repositories/$REPO_ID/docs/generate \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"types":["adr","architecture","service_doc","guidelines"],"branch":"main"}'
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
make swagger          # Gera documentação Swagger/OpenAPI via pinned swag@v1.8.12
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
│   │   │   ├── webhook.go         # GitHub webhook receiver (HMAC validation)
│   │   │   ├── analysis.go        # Code analysis + semantic search endpoints
│   │   │   ├── dependency_handler.go # Dependency scan/list endpoints
│   │   │   ├── docs.go           # Documentation generation endpoint
│   │   │   └── template.go        # AI code template generation/list/pin endpoints
│   │   ├── middleware/
│   │   │   ├── auth.go            # JWT verification, context storage
│   │   │   ├── logger.go          # Request logging
│   │   │   ├── error_handler.go   # Global error handling
│   │   │   ├── optional_auth.go   # Optional auth (no 401 on missing token)
│   │   │   └── rbac.go            # Role-based access control
│   │   ├── factories/
│   │   │   ├── make_auth_handler.go        # DI: auth service + providers
│   │   │   ├── make_repository_handler.go  # DI: repository service
│   │   │   ├── make_webhook_handler.go     # DI: webhook handler
│   │   │   ├── make_analysis_handler.go    # DI: analysis handler
│   │   │   ├── make_dependency_handler.go  # DI: dependency handler
│   │   │   ├── make_docs_handler.go        # DI: docs handler
│   │   │   └── make_template_handler.go    # DI: template handler
│   │   └── routes.go              # Route registration (/api/v1/*)
│   ├── ai/                        # Pluggable AI provider interfaces
│   │   ├── provider.go            # Analyzer + DocumentationGenerator interfaces and types
│   │   ├── generator.go           # Generator interface + template request/result types
│   │   ├── mock_analyzer.go       # Mock analyzer for testing
│   │   └── mock_generator.go      # Mock generator for testing
│   ├── dependencies/              # Package manifest parsers
│   │   ├── parser.go              # go.mod, package.json, requirements.txt, Cargo.toml parsers
│   │   └── parser_test.go         # Parser unit tests
│   ├── embeddings/                # Semantic-search provider abstraction + chunking
│   │   ├── provider.go            # Embedding Provider interface
│   │   ├── voyage.go              # Voyage AI implementation
│   │   └── chunker.go             # Temporary clone + source-code chunk extraction
│   ├── crypto/                    # Field-level encryption (AES-256-GCM)
│   │   ├── cipher.go              # Encrypt/decrypt functions
│   │   ├── cipher_test.go         # Cipher tests
│   │   └── serializer.go          # GORM hooks for transparent encryption
│   ├── integrations/
│   │   ├── anthropic/             # Anthropic (Claude) AI implementation
│   │   │   ├── client.go          # Client implementing ai.Analyzer
│   │   │   ├── documentation.go   # Client implementing ai.DocumentationGenerator
│   │   │   ├── generator.go       # Client implementing ai.Generator
│   │   │   └── *_test.go          # Anthropic analyzer/generator tests
│   │   └── github/                # GitHub API client (repos, branches, commits, PRs, contents, webhooks)
│   │       ├── client.go          # HTTP client + ClientInterface
│   │       ├── content.go         # Contents API branch/file operations
│   │       ├── pr.go              # PR-specific operations (fetch, review posting)
│   │       ├── pull_request_create.go # Create pull requests
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
│   │   ├── webhook_processor.go   # Handles webhook:process asynq task
│   │   ├── analysis_worker.go     # Handles repo:analyze asynq task (code + PR analysis)
│   │   ├── embedding_worker.go    # Handles embeddings:generate asynq task
│   │   ├── dependency_worker.go   # Handles dependency:scan asynq task
│   │   ├── docs_worker.go         # Handles docs:generate asynq task
│   │   ├── template_worker.go     # Handles template:generate asynq task
│   │   └── analysis_worker_test.go # Analysis worker tests
│   ├── models/
│   │   ├── user.go                # User with roles
│   │   ├── oauth_connection.go    # OAuth connections (provider links)
│   │   ├── auth.go                # Auth DTOs (LoginRequest, TokenResponse)
│   │   ├── repository.go          # Repository + RepositoryMetadata + StringArray
│   │   ├── dependency.go          # PackageDependency model
│   │   ├── code_template.go       # CodeTemplate model
│   │   ├── code_template_dto.go   # Template request/response DTOs
│   │   ├── doc_generation.go      # DocGeneration model
│   │   ├── doc_generation_dto.go  # Documentation request/response DTOs
│   │   ├── repository_dto.go      # CreateRepositoryRequest, UpdateRepositoryRequest
│   │   ├── webhook.go             # Webhook events + WebhookConfig + StringArray type
│   │   ├── code_analysis.go       # Code analysis results (issues, metrics, model used)
│   │   ├── code_embedding.go      # Vector embeddings (pgvector)
│   │   └── request_response.go    # Request/response DTOs (AnalyzeRepositoryRequest, JobResponse)
│   ├── config/
│   │   └── config.go              # Config struct + env loading (incl. WEBHOOK_BASE_URL)
│   ├── storage/
│   │   ├── repository.go          # Repository interface
│   │   ├── postgres/
│   │   │   ├── postgres_repository.go  # GORM implementation
│   │   │   └── doc_generation_repository.go # DocGeneration CRUD/list methods
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
│   ├── 006-encrypt-sensitive-fields.sql  # Add encrypted columns (access_token_encrypted, secret_encrypted)
│   ├── 007-add-voyage-embeddings-metadata.sql  # Voyage/pgvector semantic search metadata + VECTOR(1024)
│   ├── 008-add-organizations-multitenancy.sql  # Organizations, memberships, org config
│   ├── 009-add-package-dependencies.sql  # Package dependency inventory and CVE/update metadata
│   ├── 010-add-code-templates.sql  # Code template storage and pinning metadata
│   └── 011-add-doc-generations.sql  # Doc generation jobs, content JSONB, and PR metadata
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
- **ANTHROPIC_API_KEY** - Claude API key for code analysis, dependency analysis, documentation generation, and template generation (optional — AI features return unavailable or fail queued jobs if not set)
- **ANTHROPIC_TOKENS_PER_HOUR** - Hourly token budget for Anthropic API (default: 20000)
- **VOYAGE_API_KEY** - Voyage AI API key for semantic code search (optional — skips embedding provider if not set)
- **EMBEDDINGS_PROVIDER** - Embedding provider selector (default: voyage)
- **EMBEDDINGS_MODEL** - Embedding model (default: voyage-code-3)
- **EMBEDDINGS_DIMENSIONS** - Embedding vector dimension (default: 1024)
- **GITHUB_TOKEN** - GitHub personal access token (required for webhook registration, PR operations, documentation PR creation, and private repository clones for metrics/dependency scans/embeddings)
- **GITHUB_PR_REVIEW_ENABLED** - Post AI-generated PR reviews to GitHub (default: false)
- **WEBHOOK_BASE_URL** - Public URL for webhook registration (e.g., ngrok URL; leave empty or use localhost to skip)
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
- **PackageDependency** - Dependências de pacotes com versões, ecossistema, CVEs e status de atualização
- **CodeTemplate** - Templates de código gerados por IA com arquivos JSONB, snapshot de stack, status e metadados de pinning
- **DocGeneration** - Geração de documentação com conteúdo Markdown JSONB, branch/PR criado, status, tokens e erros

### Database
- **Tabelas principais** com indexes otimizados para auth, repositórios, webhooks, análises, embeddings, dependências, docs e templates
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
CreateCodeEmbedding, CreateCodeEmbeddings, SearchEmbeddings, DeleteEmbeddings

// Package Dependencies
UpsertPackageDependency, ListPackageDependencies,
UpdatePackageDependencyVulnStatus, DeletePackageDependencies

// Code Templates
CreateCodeTemplate, GetCodeTemplate, UpdateCodeTemplate, ListCodeTemplates

// Doc Generations
CreateDocGeneration, UpdateDocGeneration, GetDocGeneration,
GetLatestDocGenerationForRepo, ListDocGenerationsForRepo
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
- Payload: base64url(json{"nonce":"<random>","organization_id|organization_name":"...","exp":<unix>})
- Signature: base64url(HMAC-SHA256(payload, jwtSecret))
- Format: <payload>.<signature>
- Expiry: 10 minutes
```

### Multi-Organization Login
```
POST /auth/login with email/password:
  - one org: returns TokenResponse directly
  - multiple orgs: returns 202 with requires_organization_selection, login_ticket, organizations[]

POST /auth/select-organization:
  - accepts login_ticket + organization_id
  - validates ticket and membership before issuing TokenResponse
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
  TypeSyncRepo, TypeAnalyzeRepo, TypeProcessWebhook, TypeGenerateEmbeddings, TypeScanDependencies, TypeGenerateDocs, TypeGenerateTemplate

Key builders (internal/storage/redis/keys.go):
  TokenKey(jti), UserKey(id), RepoKey(id), SessionKey(id)
```

### Auto-Generated Documentation
```
Generate:
  POST /api/v1/repositories/:id/docs/generate
  Body: {"types":["adr","architecture","service_doc","guidelines"],"branch":"main"}
  Creates a pending DocGeneration and enqueues TypeGenerateDocs with manual TaskID deduplication per repository

Worker:
  Clones the repository shallowly, gathers directory/key-file/commit/PR/latest-analysis context, asks Claude for Markdown, commits docs to a docs/auto-generated-* branch, and opens a GitHub PR

Storage:
  doc_generations.content is JSONB map[string]string keyed by doc type
  Generated content is reused by AnalysisWorker as PROJECT STANDARDS / DOCUMENTATION in future prompts

Generated paths:
  adr -> docs/adr/README.md
  architecture -> docs/ARCHITECTURE.md
  service_doc -> docs/SERVICE.md
  guidelines -> CONTRIBUTING.md
```

### Intelligent Code Templates
```
Generate:
  POST /api/v1/templates
  POST /api/v1/repositories/:id/templates
  Body: {"prompt":"Create CRUD in Next.js with auth","stack_hint":"Next.js 14, Tailwind"}
  Creates a pending CodeTemplate and enqueues TypeGenerateTemplate with manual TaskID deduplication per template

Poll:
  GET /api/v1/templates/:id
  Status values: pending, generating, completed, failed
  Completed templates include summary, files[], ai_model, tokens_used, processing_ms, and stack_snapshot

List:
  GET /api/v1/templates?pinned=true&status=completed&limit=20&offset=0
  Lists templates scoped to the authenticated organization

Pin:
  PATCH /api/v1/templates/:id/pin
  Body: {"is_pinned":true,"name":"Go CRUD Template"}

Storage:
  code_templates.files is JSONB containing generated files with path, content, and language
  code_templates tokens are included in the shared Anthropic hourly budget
```

### Dependency Tracking
```
Supported manifests:
  go.mod, package.json, requirements.txt, Cargo.toml

Scan:
  POST /api/v1/repositories/:id/dependencies/scan
  Enqueues TypeScanDependencies with manual deduplication for 10 minutes
  Worker clones repository, parses manifests, upserts package_dependencies, and sends manifests to Claude

List:
  GET /api/v1/repositories/:id/dependencies?vulnerable=true
  Returns package inventory with current/latest versions, ecosystem, manifest file, CVEs, and vulnerability flags

Webhook auto-trigger:
  push and pull_request events enqueue dependency scans only when supported manifest files changed
```

### Semantic Search
```
Provider:
  Voyage AI through internal/embeddings.Provider
  Default model: voyage-code-3
  Default dimension: 1024

Indexing:
  POST /api/v1/repositories/:id/embeddings
  Enqueues TypeGenerateEmbeddings
  Worker clones repository temporarily, chunks source files, embeds chunks with input_type=document
  Replaces old embeddings for same repo/provider/model/dimension/branch

Query:
  GET /api/v1/repositories/:id/search?q=<query>&limit=10&min_score=0.55
  Embeds query with input_type=query
  Searches code_embeddings with pgvector cosine distance plus textual boosts for content, file path, and language
  Filters out low-confidence matches below min_score; default min_score is 0.55
  Returns file path, content snippet, line range, language, score, provider, model, branch
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
- [x] Real PR diff analysis for pull_request webhooks
- [x] Organization onboarding + multi-org login selection flow
- [x] Code analysis API + Claude integration — `TypeAnalyzeRepo` job with pluggable AI providers
- [x] Semantic search with Voyage AI + pgvector embeddings — `TypeGenerateEmbeddings` job
- [x] Dependency tracking — manifest parsing, `TypeScanDependencies` job, Claude CVE/update analysis, dependency endpoints
- [x] Auto-generated documentation — `TypeGenerateDocs`, GitHub Contents/PR delivery, doc-aware analysis prompts
- [x] Rate limiting & request throttling for Anthropic-backed manual triggers
- [ ] Integration tests for handlers and postgres repository (requires test DB)

## 🤝 Contribuindo

Por favor, veja [CONTRIBUTING.md](docs/CONTRIBUTING.md) (a criar) para guidelines.

## 📄 License

MIT

## 📞 Contato

Para dúvidas ou sugestões, abra uma issue ou entre em contato com o time.

---

**Status**: 🤖 AI Integration + Semantic Search + Dependency Tracking + Auto Docs Complete (Auth + Sync + Webhook + Encryption + Real PR Diff Analysis + Dedup + Rate Limiting + Metrics + Voyage embeddings + package dependency scans + documentation PRs)
**Última atualização**: April 30, 2026 (Auto docs: doc generation worker, endpoint, storage, GitHub Contents/PR delivery, doc-aware analysis prompts)

### 📖 Accessing the API Documentation
```bash
make dev
# Open browser: http://localhost:3000/swagger/index.html
```
