# IDP Backend - Internal Developer Platform

Internal Developer Platform (IDP) backend with JWT authentication, multi-provider OAuth 2.0 (GitHub, GitLab), a repository catalog with GitHub and GitLab sync, pull/merge request browsing, spatial repository relationship mapping, CI coverage ingestion, and AI-generated project documentation. Built with Go and PostgreSQL.

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
- ✅ 23 SQL migrations (schema, auth, oauth_connections, refresh token rotation, encryption fields, multitenancy, doc generations, repository relationships, repository list enrichment indexes, organization output language, coverage uploads and tokens, AI feature removal)
- ✅ Migration tracking via `schema_migrations` table (no re-runs on restart)
- ✅ Baseline detection for existing databases (safe upgrade path)
- ✅ OAuth connections table (provider + provider_user_id uniqueness)
- ✅ Soft deletes (deleted_at timestamps)
- ✅ Audit triggers (created_at, updated_at automation)
- ✅ Encrypted fields: OAuth tokens (access_token_encrypted), webhook secrets (secret_encrypted)
- ✅ Package dependency inventory with CVE/update metadata
- ✅ Repository relationship graph storage for spatial navigation maps
- ✅ Doc generation storage with generated Markdown content as JSONB and PR metadata
- ✅ Optimized indexes for enriched repository list queries with LATERAL joins

### Repository Management
- ✅ CRUD endpoints — create, list, get, update, delete repositories
- ✅ Enriched list response — latest CI coverage per repository via a LATERAL join, in a single query (no N+1)
- ✅ GitHub and GitLab sync — fetches metadata (branches, commits, PRs/MRs, languages, stars, forks) through the provider a repository's URL resolves to
- ✅ Sync status lifecycle — `idle → syncing → synced / error`
- ✅ WebhookConfig — registers GitHub webhook on sync, stores HMAC secret
- ✅ Webhook registration skipped automatically on localhost (use ngrok for local dev)

### Spatial Repository Navigation
- ✅ Canonical `repository_relationships` model for directed repo-to-repo relationships
- ✅ Relationship kinds: `http`, `async`, `library`, `data`, `infra`, `manual`, `other`
- ✅ Relationship source/confidence fields for manual data now and inferred data later
- ✅ Graph endpoint: `GET /repositories/graph` returns all organization repositories as nodes, including independent repos
- ✅ Relationship CRUD endpoints: `POST/PATCH/DELETE /repository-relationships`
- ✅ Legacy `repository_dependencies` backfilled into relationship edges by migration `012`

### Webhook Pipeline
- ✅ HMAC-SHA256 signature validation (X-Hub-Signature-256)
- ✅ Idempotency via `X-GitHub-Delivery` ID — duplicate deliveries return 200
- ✅ Events persisted to `webhooks` table with status tracking and retry logic
- ✅ Background processing worker (`webhook:process` asynq task)

### API Routes
- ✅ Public routes: login, organization selection, register, token refresh, OAuth (GitHub/GitLab)
- ✅ Public webhook receivers: `POST /api/v1/webhooks/github/:repoID` (HMAC-SHA256 signature) and `POST /api/v1/webhooks/gitlab/:repoID` (`X-Gitlab-Token` shared secret, compared in constant time)
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
- ✅ Comprehensive annotations for auth, repository, pull request, webhook, coverage, documentation, and health endpoints
- ✅ Documentation generation endpoint annotation
- ✅ JWT security scheme documented
- ✅ Automatic generation with `make swagger`

### Documentation Generation (AI)
- ✅ Pluggable `ai.DocumentationGenerator` interface — the platform's only LLM-backed feature
- ✅ Anthropic (Claude) implementation with the Anthropic SDK
- ✅ HTTP endpoints: `POST /repositories/:id/docs/generate`, `POST /organizations/docs/generate`
- ✅ Supported doc types: `adr`, `architecture`, `service_doc`, `guidelines`
- ✅ Async flow: create `doc_generations` row → enqueue `docs:generate` → clone repo → generate Markdown → commit files → open GitHub PR
- ✅ GitHub Contents API integration for branch creation and create/update file commits
- ✅ Generated files: `docs/adr/README.md`, `docs/ARCHITECTURE.md`, `docs/SERVICE.md`, `CONTRIBUTING.md`
- ✅ Org-wide docs build on an aggregated organization snapshot (repos, dominant stacks, relationships, existing per-repo docs)
- ✅ Generated Markdown stored in PostgreSQL JSONB; editable in-app via `PATCH /docs/:id`
- ✅ Token rate limiting: hourly budget (default 20K tokens/hour, configurable via `ANTHROPIC_TOKENS_PER_HOUR`), accounted from `doc_generations`
- ✅ **Configurable output language** (org-level): `OrganizationConfig.OutputLanguage` (BCP 47, default `en`) drives a System prompt so generated Markdown comes back in the chosen language. Validated by `golang.org/x/text/language` and surfaced via `PATCH /organizations/configs`.

### Coverage via CI Upload
- ✅ Endpoint `POST /api/v1/repositories/:id/coverage` recebe relatórios brutos (Go cover, LCOV, Cobertura XML, JaCoCo XML) com até 5 MB
- ✅ Autenticação via Bearer `cov_*` token, escopo por repositório, hash SHA-256 em repouso, exibido só uma vez
- ✅ CRUD de tokens em `POST/GET/DELETE /api/v1/repositories/:id/coverage/tokens` (auth via JWT)
- ✅ Cada upload é persistido em `coverage_uploads` keyed por `(repo, commit_sha)`; histórico mantido para auditoria
- ✅ A listagem enriquecida de repositórios lê o upload mais recente via LATERAL join e expõe `stats.has_coverage` para distinguir “nunca medido” de “medido 0%”

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

### 4. Upload coverage from CI (optional)

Generate an upload token (one-time visibility):
```bash
TOKEN=$(curl -sX POST http://localhost:3000/api/v1/repositories/$REPO_ID/coverage/tokens \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{"name":"github-actions"}' | jq -r '.token')
echo "$TOKEN"   # cov_<hex...>  — store as a CI secret immediately
```

Upload a report from CI (any of: `go`, `lcov`, `cobertura`, `jacoco`):
```bash
go test ./... -coverprofile=coverage.out
curl -X POST http://localhost:3000/api/v1/repositories/$REPO_ID/coverage \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Coverage-Format: go" \
  -H "X-Commit-SHA: $(git rev-parse HEAD)" \
  -H "Content-Type: application/octet-stream" \
  --data-binary @coverage.out
```

Response: `200 OK` with `{ lines_covered, lines_total, percentage, status }`.

The most recent upload per repository is what the enriched repository list
surfaces as `stats`. Multiple uploads for the same `(repo, sha)` are all
retained for audit; the newest one wins. A repository with no upload at all
reports `has_coverage: false`, which the UI renders as "not configured"
rather than a misleading
`UPDATE` of the metrics on the existing row. Without an upload, the quality
score skips coverage deductions (no false penalty).

### 5. Test the server
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

# Add a repository (triggers a sync against its provider automatically)
curl -X POST http://localhost:3000/api/v1/repositories \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://github.com/owner/repo"}'

# List repositories
curl -H "Authorization: Bearer $TOKEN" http://localhost:3000/api/v1/repositories

# Create a repository relationship for the spatial map
curl -X POST http://localhost:3000/api/v1/repository-relationships \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"source_repository_id":"REPO_A","target_repository_id":"REPO_B","kind":"http","label":"REST API","metadata":{"protocol":"rest","endpoint":"/v1/payments"}}'

# Fetch graph nodes/edges for spatial navigation
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:3000/api/v1/repositories/graph?include_metadata=true"

# OAuth onboarding: redirect to GitHub (if configured)
curl -L "http://localhost:3000/api/v1/auth/github?organization_name=Acme%20Inc"
```

### 6. Generate repository documentation
```bash
# Requires ANTHROPIC_API_KEY, the provider token for the repository's host, and Redis/asynq enabled
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
make test-e2e         # Suíte end-to-end (precisa de docker)
make test-e2e-live    # Testes opt-in contra a API real do gitlab.com
make e2e-stack        # Deixa a stack E2E aberta (para o Playwright do frontend)
make fake-gitlab      # Sobe só o GitLab falso, para inspecionar respostas à mão
make lint             # Roda linter
```

#### End-to-end

Duas camadas, ambas locais e sem depender de conta em provedor nenhum.

**Backend black-box** (`make test-e2e`) sobe Postgres e Redis em containers
descartáveis — portas sorteadas, não encostam no `docker compose` de
desenvolvimento —, compila e roda o binário real do servidor, e substitui o
provedor por um GitLab falso (`internal/testsupport/fakegitlab`) que serve
payloads capturados do gitlab.com. Cobre onboarding, token na organização,
repositório de grupo aninhado, sync populando catálogo e detecção de CI/testes
por árvore paginada, scorecard, registro de webhook, entrega disparada pelo
provedor (com replay e token errado), navegação de merge requests, e os caminhos
de erro: sem token, host sem client, árvore truncada, organização alheia.

**Navegador** — as specs Playwright vivem no repo do frontend e rodam contra a
mesma stack, mantida aberta por `make e2e-stack` (backend em `:3000`, GitLab
falso em `:8081`). O token a colar em Configurações → GitLab é
`glpat-e2e-token`; os projetos servidos são
`gitlab.com/gitlab-org/nested-group/gitlab-runner` e
`gitlab.com/gitlab-org/huge-monorepo` (esse com árvore infinita, para exercitar
truncamento).

**Contra o gitlab.com real** (`make test-e2e-live`) prova que os shapes gravados
continuam sendo o que o GitLab manda. Anônimo, só leitura de projeto público,
precisa de rede.

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
│   ├── ai/
│   │   ├── provider.go            # DocumentationGenerator interface + request/result types
│   │   └── mock_documentation.go  # Mock generator for testing
│   ├── api/
│   │   ├── factories/             # Dependency injection wiring per handler
│   │   ├── handlers/
│   │   │   ├── auth.go            # Login, register, OAuth, logout, /users/me
│   │   │   ├── coverage.go        # CI coverage ingestion + upload tokens
│   │   │   ├── docs.go            # Repo/org documentation generation, listing, editing
│   │   │   ├── organization_config.go # Org settings (keys, budget, output language)
│   │   │   ├── pull_requests.go   # Read-only GitHub PR list/detail/files
│   │   │   ├── repository.go      # Repository CRUD endpoints
│   │   │   ├── repository_relationship.go # Spatial graph + repo relationship endpoints
│   │   │   └── webhook.go         # GitHub webhook receiver (HMAC validation)
│   │   ├── middleware/            # JWT auth, RBAC, logging, error handling
│   │   └── routes.go              # Route table
│   ├── config/                    # Env-driven configuration
│   ├── coverage/                  # Coverage report parsers (go, lcov, cobertura, jacoco)
│   ├── crypto/                    # AES-256-GCM cipher + GORM serializer
│   ├── docs/                      # Static org documentation templates (ADR, etc.)
│   ├── i18n/                      # BCP 47 output-language resolution
│   ├── integrations/
│   │   ├── anthropic/
│   │   │   ├── client.go          # Anthropic SDK client
│   │   │   ├── documentation.go   # Repo + org documentation prompts
│   │   │   ├── org_context.go     # Aggregated org snapshot for org-wide docs
│   │   │   └── system_prompt.go   # Output-language System prompt
│   │   └── github/                # REST client, PR reads, contents/PR writes, webhooks
│   ├── jobs/                      # asynq enqueuer, worker, task types
│   ├── models/                    # GORM models + DTOs
│   ├── oauth/                     # GitHub/GitLab OAuth providers
│   ├── services/                  # Auth, repository, relationship, sync, coverage
│   ├── storage/
│   │   ├── postgres/              # PostgreSQL repository implementation
│   │   └── redis/                 # Redis client, cache, key builders
│   ├── utils/                     # Logging, auth context, URL parsing
│   └── workers/
│       ├── sync_worker.go         # Handles repo:sync asynq task
│       ├── docs_worker.go         # Handles docs:generate / docs:generate_org tasks
│       └── webhook_processor.go   # Handles webhook:process asynq task
├── migrations/                    # Sequential SQL migrations (001 … 023)
├── docs/                          # Generated Swagger (docs.go, swagger.json, swagger.yaml)
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
- **ANTHROPIC_API_KEY** - Claude API key for documentation generation (optional — doc generation returns unavailable or fails queued jobs if not set)
- **ANTHROPIC_TOKENS_PER_HOUR** - Hourly token budget for Anthropic API (default: 20000)
- **GITHUB_TOKEN** - GitHub personal access token (required for webhook registration, PR reads, documentation PR creation, and private repository clones during doc generation)
- **GITLAB_TOKEN** - GitLab personal access token with the `api` scope, used for the same operations on gitlab.com projects. Both provider tokens are platform-level fallbacks: an organization's own `github_token` / `gitlab_token` takes precedence, and a repository is only ever queried with its own host's token.
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
- **Repository** - Repositórios sincronizados do provider, com metadata JSONB
- **Webhook** - Webhooks com retry logic e status de processamento
- **RepositoryRelationship** - Relacionamentos direcionais entre repositórios para mapa espacial
- **CoverageUpload** - Relatórios de cobertura enviados pelo CI, com granularidade por arquivo
- **CoverageUploadToken** - Tokens `cov_*` por repositório, hasheados em repouso
- **DocGeneration** - Geração de documentação com conteúdo Markdown JSONB, branch/PR criado, status, tokens e erros

### Database
- **Tabelas principais** com indexes otimizados para auth, repositórios, webhooks, coverage, relacionamentos e docs
- **JSONB** para dados flexíveis (metadata, issues, métricas)
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

// Repository Relationships
CreateRepositoryRelationship, GetRepositoryRelationship,
UpdateRepositoryRelationship, DeleteRepositoryRelationship,
ListRepositoryRelationships

// WebhookConfigs
GetWebhookConfigByRepoID, CreateWebhookConfig, UpdateWebhookConfig

// Webhooks (events)
GetWebhook, GetWebhookByDeliveryID, CreateWebhook, UpdateWebhook,
ListPendingWebhooks, ListFailedWebhooks

// Coverage
CreateCoverageUpload, GetLatestCoverageUpload, ListCoverageUploadsForCommit,
CreateCoverageUploadToken, GetCoverageUploadTokenByHash,
ListCoverageUploadTokens, RevokeCoverageUploadToken

// AI token accounting (documentation generation)
SumTokensUsedSince

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
  TypeSyncRepo, TypeProcessWebhook, TypeGenerateDocs, TypeGenerateOrgDocs

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
  Clones the repository shallowly, gathers directory/key-file/commit/PR context, asks Claude for Markdown, commits docs to a docs/auto-generated-* branch, and opens a GitHub PR

Storage:
  doc_generations.content is JSONB map[string]string keyed by doc type
  Generated Markdown is stored in doc_generations.content and editable in-app via PATCH /docs/:id

Generated paths:
  adr -> docs/adr/README.md
  architecture -> docs/ARCHITECTURE.md
  service_doc -> docs/SERVICE.md
  guidelines -> CONTRIBUTING.md
```

### Coverage via CI Upload
```
Tokens (managed by repository owners):
  POST   /api/v1/repositories/:id/coverage/tokens     # JWT, returns plaintext ONCE
  GET    /api/v1/repositories/:id/coverage/tokens     # JWT
  DELETE /api/v1/repositories/:id/coverage/tokens/:tokenID

Upload (called by CI):
  POST /api/v1/repositories/:id/coverage
  Authorization: Bearer cov_<hex...>
  X-Coverage-Format: go|lcov|cobertura|jacoco
  X-Commit-SHA: <40-char hex>
  X-Coverage-Branch: <optional>
  Content-Type: application/octet-stream
  Body: raw report bytes (max 5 MB)
  → 200 { id, commit_sha, lines_covered, lines_total, percentage, status }

Storage:
  coverage_uploads (durable, history kept) keyed by (repository_id, commit_sha, created_at DESC)

List integration:
  The enriched repository query LATERAL-joins the newest coverage_uploads row per repo
  Uploads are never mutated; the newest row simply wins

Reporting:
  stats.has_coverage is false when a repository has never uploaded — the UI shows
  "not configured" instead of a misleading 0%
```

### Spatial Repository Navigation
```
Graph:
  GET /api/v1/repositories/graph?kind=http&repository_id=<repo>&include_metadata=true
  Returns all organization repositories as nodes and matching repository_relationships as edges
  Independent repositories are included as nodes even when they have no edges

Create relationship:
  POST /api/v1/repository-relationships
  Body: {"source_repository_id":"repo-a","target_repository_id":"repo-b","kind":"http","label":"REST API","metadata":{"protocol":"rest"}}
  Direction is source -> target. Manual creates use source=manual and confidence=1.0.

Kinds:
  http, async, library, data, infra, manual, other

Storage:
  repository_relationships is the canonical graph table
  repository_dependencies remains legacy; migration 012 backfills it as source=legacy_dependency
```

### pgx/v5 Migration Quirk
- pgx/v5 does NOT support multiple SQL statements in `db.Exec()`
- Solution: Use underlying `*sql.DB` from `db.DB()` to run full migration files
- Migration runner uses `sqlDB.Exec(fileContent)` not `gorm.DB.Exec()`

### .env Loading
- Use `godotenv.Load()` in `main.go` before `config.Load()`
- Go does NOT load .env automatically

## 🎯 Next Steps

- [x] Repository management endpoints (CRUD + GitHub/GitLab sync)
- [x] Webhook pipeline (GitHub HMAC ingestion + background processing)
- [x] Encryption for sensitive fields (OAuth tokens, webhook secrets)
- [x] API documentation (Swagger/OpenAPI)
- [x] Organization onboarding + multi-org login selection flow
- [x] Pull request browsing — list, detail and diff straight from the GitHub API
- [x] Auto-generated documentation — `TypeGenerateDocs` / `TypeGenerateOrgDocs`, GitHub Contents/PR delivery, in-app Markdown editing
- [x] Spatial repository navigation — `repository_relationships`, graph endpoint, relationship CRUD, legacy dependency backfill
- [x] Coverage via CI upload — `cov_*` tokens, Go/LCOV/Cobertura/JaCoCo parsers, per-repo coverage in the enriched list
- [x] Rate limiting & request throttling for Anthropic-backed doc generation
- [x] Removal of the AI-driven features (code review, semantic search, code templates) — migration `023`
- [ ] Integration tests for handlers and postgres repository (requires test DB)

## 🤝 Contribuindo

Por favor, veja [CONTRIBUTING.md](docs/CONTRIBUTING.md) (a criar) para guidelines.

## 📄 License

MIT

## 📞 Contato

Para dúvidas ou sugestões, abra uma issue ou entre em contato com o time.

---

**Status**: Plain IDP — Auth + Repository Catalog + GitHub Sync + Webhooks + Encryption + Pull Request Browsing + Spatial Repository Graph + Coverage via CI Upload + AI Documentation Generation
**Última atualização**: August 20, 2026 (Removed the AI-driven features — PR review findings, semantic search with Voyage embeddings, and AI code templates. Documentation generation remains the one LLM-backed feature. Migration `023` drops `code_analyses`, `code_templates`, `code_embeddings` and their columns.)

### 📖 Accessing the API Documentation
```bash
make dev
# Open browser: http://localhost:3000/swagger/index.html
```
