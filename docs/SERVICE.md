# IDP Backend Service Documentation

## Overview

**IDP Backend** is an Internal Developer Platform (IDP) written in Go that provides a unified interface for managing development infrastructure across multiple Git providers. It enables organizations to maintain a centralized repository catalog, sync metadata from GitHub and GitLab, track spatial repository relationships, ingest CI coverage reports, and generate AI-powered project documentation.

The platform is built on a modern stack: **Go 1.25+**, **PostgreSQL 15+** with pgvector support, **Redis 7+** for caching and job queuing, and integrates with external providers (GitHub, GitLab, Anthropic Claude) via OAuth 2.0 and REST APIs.

### Core Capabilities

- **Multi-Provider Authentication**: Email/password and OAuth 2.0 (GitHub, GitLab) with refresh token rotation and reuse detection
- **Repository Management**: CRUD operations with automatic sync to GitHub/GitLab, webhook integration, and relationship mapping
- **Spatial Navigation**: Repository-to-repository relationship tracking for dependency visualization
- **CI Coverage Ingestion**: HTTP endpoints accepting Go cover, LCOV, Cobertura, and JaCoCo formats
- **AI Documentation**: Async document generation (ADRs, architecture guides, service docs) via Anthropic Claude with language localization
- **Multi-Tenancy**: Organization-scoped data with team management and role-based access control
- **Encryption**: AES-256-GCM for sensitive fields (OAuth tokens, webhook secrets) with key rotation support

## Prerequisites

### System Requirements
- **Go**: 1.25.2 or later
- **Docker & Docker Compose**: Latest stable versions
- **Git**: For repository operations and integration testing
- **Make**: For running build and test targets

### External Accounts (Optional)
- **GitHub**: OAuth application credentials and personal access token (for webhook registration and PR operations)
- **GitLab**: OAuth application credentials and personal access token (for sync, MR reads, webhooks)
- **Anthropic**: API key for documentation generation (optional—documentation features are skipped if not configured)

### Runtime Dependencies
- **PostgreSQL 15+**: Primary database with pgvector extension
- **Redis 7+**: Cache layer and job queue (gracefully degrades if unavailable)
- **ngrok** (local development): For exposing localhost webhooks to GitHub

## Environment Variables

Copy `.env.example` to `.env` and configure the following:

### Database
```
DB_HOST=localhost           # PostgreSQL hostname
DB_PORT=5432                # PostgreSQL port
DB_USER=postgres            # PostgreSQL username
DB_PASSWORD=postgres        # PostgreSQL password
DB_NAME=idp_dev             # PostgreSQL database name
```

### Cache & Queue
```
REDIS_HOST=localhost        # Redis hostname
REDIS_PORT=6379             # Redis port
```

### JWT & Tokens
```
JWT_SECRET=your-super-secret-key-change-this-in-production
JWT_ISSUER=idp-backend
JWT_AUDIENCE=idp-users
ACCESS_TOKEN_TTL=15         # Minutes
REFRESH_TOKEN_TTL=10080     # Minutes (7 days)
```

### Encryption
```
ENCRYPTION_KEY=             # Base64-encoded 32-byte key (generate: openssl rand -base64 32)
```

### OAuth Providers
```
GITHUB_CLIENT_ID=           # GitHub OAuth app client ID
GITHUB_CLIENT_SECRET=       # GitHub OAuth app client secret
GITHUB_CALLBACK_URL=http://localhost:3000/api/v1/auth/github/callback

GITLAB_CLIENT_ID=           # GitLab OAuth app client ID
GITLAB_CLIENT_SECRET=       # GitLab OAuth app client secret
GITLAB_CALLBACK_URL=http://localhost:3000/api/v1/auth/gitlab/callback
GITLAB_BASE_URL=            # Self-hosted GitLab API root (empty = https://gitlab.com/api/v4)
```

### API & Webhooks
```
GITHUB_TOKEN=               # GitHub personal access token (required for webhook registration & PR operations)
GITLAB_TOKEN=               # GitLab personal access token with api scope (fallback for org-less sync, webhooks, doc MRs)
WEBHOOK_BASE_URL=           # Public URL for webhook registration (e.g., https://abc123.ngrok.io)
                             # Leave empty to skip GitHub webhook registration
```

### AI Features
```
ANTHROPIC_API_KEY=          # Anthropic API key (optional—skip docs generation if not set)
ANTHROPIC_TOKENS_PER_HOUR=20000  # Hourly token budget
```

### Logging
```
LOG_LEVEL=info              # Log level: debug, info, warn, error
```

## How to Run Locally

### 1. Clone and Setup
```bash
git clone https://github.com/paulozy/cuddly-lamp.git
cd cuddly-lamp
cp .env.example .env
# Edit .env with your GitHub/GitLab credentials and secrets
```

### 2. Start Infrastructure Services
```bash
make docker-up
```

This starts PostgreSQL and Redis in Docker containers with health checks. The server waits for both to be ready before booting.

### 3. Run the Server
```bash
make dev
```

The server:
- Loads environment variables from `.env`
- Applies pending migrations (idempotently)
- Registers OAuth providers if configured
- Starts HTTP server on `http://localhost:3000`
- Initializes background workers (repo sync, webhook processing)

### 4. Verify Health
```bash
curl http://localhost:3000/api/v1/health
```

Expected response:
```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### 5. Test Authentication
```bash
# Register a user
curl -X POST http://localhost:3000/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "full_name": "Test User",
    "password": "SecurePassword123",
    "organization_name": "My Org"
  }'

# Login
curl -X POST http://localhost:3000/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "SecurePassword123"
  }' | jq '.access_token'
```

## How to Run with Docker

### Option 1: Full Stack with Docker Compose

The repository includes a `docker-compose.yml` that starts PostgreSQL and Redis:

```bash
docker compose up -d
```

This is sufficient for local development. The Go application runs on your host machine for rapid iteration.

### Option 2: Production-Grade Containerization

Create a `Dockerfile` in the repository root (not included in the sample but follow this pattern):

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o idp-server cmd/server/main.go

FROM alpine:3.18
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /app/idp-server /usr/local/bin/
EXPOSE 3000
CMD ["idp-server"]
```

Build and run:
```bash
docker build -t idp-backend:latest .
docker run -p 3000:3000 \
  --env-file .env \
  --network idp-network \
  idp-backend:latest
```

### Option 3: Docker Compose with Application

Extend `docker-compose.yml` to include the application service:

```yaml
services:
  postgres:
    # ... existing configuration
  redis:
    # ... existing configuration
  app:
    build: .
    ports:
      - "3000:3000"
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    environment:
      DB_HOST: postgres
      REDIS_HOST: redis
    networks:
      - idp-network
```

Run entire stack:
```bash
docker compose up -d
```

## API Endpoints

### Authentication
- `POST /api/v1/auth/register` — User registration with organization creation
- `POST /api/v1/auth/login` — Email/password login (returns access token + optional org selection)
- `POST /api/v1/auth/select-organization` — Multi-org selection with login ticket
- `POST /api/v1/auth/token/refresh` — Refresh access token using refresh token
- `POST /api/v1/auth/logout` — Revoke access token and refresh token family
- `GET /api/v1/auth/github` — Redirect to GitHub OAuth
- `GET /api/v1/auth/gitlab` — Redirect to GitLab OAuth
- `GET /api/v1/auth/github/callback` — GitHub OAuth callback handler
- `GET /api/v1/auth/gitlab/callback` — GitLab OAuth callback handler

### User
- `GET /api/v1/users/me` — Fetch current authenticated user

### Repositories
- `GET /api/v1/repositories` — List repositories (with enriched coverage stats via LATERAL join)
- `POST /api/v1/repositories` — Create repository (triggers automatic sync)
- `GET /api/v1/repositories/:id` — Fetch repository details
- `PATCH /api/v1/repositories/:id` — Update repository
- `DELETE /api/v1/repositories/:id` — Delete repository
- `GET /api/v1/repositories/graph` — Fetch repository graph (nodes + relationships)

### Repository Relationships
- `POST /api/v1/repository-relationships` — Create dependency relationship
- `GET /api/v1/repository-relationships` — List relationships
- `PATCH /api/v1/repository-relationships/:id` — Update relationship
- `DELETE /api/v1/repository-relationships/:id` — Delete relationship

### CI Coverage
- `POST /api/v1/repositories/:id/coverage` — Upload coverage report (any format)
- `POST /api/v1/repositories/:id/coverage/tokens` — Create upload token (one-time view)
- `GET /api/v1/repositories/:id/coverage/tokens` — List coverage tokens
- `DELETE /api/v1/repositories/:id/coverage/tokens/:tokenId` — Revoke token

### Documentation Generation
- `POST /api/v1/repositories/:id/docs/generate` — Generate docs for repository
- `POST /api/v1/organizations/docs/generate` — Generate org-wide documentation
- `GET /api/v1/docs/:id` — Fetch generated documentation
- `PATCH /api/v1/docs/:id` — Update generated documentation

### Webhooks
- `POST /api/v1/webhooks/github/:repoId` — GitHub webhook receiver (HMAC-SHA256 validation)
- `POST /api/v1/webhooks/gitlab/:repoId` — GitLab webhook receiver (token validation)

### Health
- `GET /api/v1/health` — Service health check

### Swagger/OpenAPI
- `GET /swagger/index.html` — Interactive Swagger UI
- `GET /swagger/swagger.json` — OpenAPI specification (JSON)

## Running Tests

### Unit Tests
```bash
make test
```

Runs all tests with verbose output. Includes unit tests for auth, repositories, coverage, integrations.

### Coverage Report
```bash
make test-coverage
```

Generates `coverage.html` with code coverage metrics.

### End-to-End Tests
```bash
make test-e2e
```

Runs the e2e suite (requires Docker). Boots real PostgreSQL and Redis containers, a fake GitLab server with recorded payloads, and the real server binary. Timeout: 20 minutes.

### Live Provider Tests
```bash
GITLAB_LIVE_TEST=1 make test-e2e-live
```

Tests against real `gitlab.com` to validate recorded webhook payloads still match current provider behavior. Requires network access.

### E2E Stack (Manual Testing)
```bash
make e2e-stack
```

Boots PostgreSQL, Redis, fake GitLab, and the real server for interactive browser testing and manual poking. Press Ctrl+C to tear down.

## Key Dependencies

### Core Framework & HTTP
- **gin-gonic/gin** (v1.12.0): Web framework
- **swaggo/swag** (v1.8.12): Swagger/OpenAPI documentation

### Database & ORM
- **jackc/pgx/v5** (v5.9.2): PostgreSQL driver
- **gorm.io/gorm** (v1.31.1): ORM with PostgreSQL dialect
- **gorm.io/datatypes** (v1.2.7): GORM extensions

### Authentication & Security
- **golang-jwt/jwt/v5** (v5.3.1): JWT token creation and validation
- **golang.org/x/crypto** (v0.50.0): Argon2 password hashing and cryptographic primitives
- **golang.org/x/oauth2** (v0.36.0): OAuth 2.0 client support

### Cache & Queue
- **redis/go-redis/v9** (v9.18.0): Redis client with connection pooling
- **hibiken/asynq** (v0.26.0): Distributed task queue (job scheduling, retries, cron)
- **alicebob/miniredis/v2** (v2.37.0): In-memory Redis for testing

### External Integrations
- **go-git/go-git/v5** (v5.18.0): Git operations (clone, commit, branch)
- **anthropics/anthropic-sdk-go** (v1.38.0): Anthropic Claude API client
- **google/uuid** (v1.6.0): UUID generation

### Utilities & Observability
- **go.uber.org/zap** (v1.27.1): Structured logging
- **joho/godotenv** (v1.5.1): .env file loading
- **golang.org/x/text** (v0.36.0): Internationalization and language validation

### Development & Testing
- **go.uber.org/zap**: Structured logging
- **golang.org/x/tools** (v0.43.0): Go tools and analyzers

Full dependency tree available in `go.mod` and `go.sum`.

## Known Issues & Limitations

### 1. Webhook Registration Requires Public URL
Webhook registration with GitHub is skipped if `WEBHOOK_BASE_URL` is not set or points to localhost. Use **ngrok** or similar to expose local development to GitHub:
```bash
ngrok http 3000
# Set WEBHOOK_BASE_URL=https://abc123.ngrok.io in .env
```

### 2. Optional Providers Gracefully Degrade
If `GITHUB_TOKEN` or `GITLAB_TOKEN` are not configured, repository sync and webhook operations for those providers will fail silently or return limited metadata. Ensure tokens are set before syncing private repositories or registering webhooks.

### 3. Redis is Optional but Recommended
The service boots without Redis (cache and job queue degrade to no-op). Background workers (repository sync, webhook processing) require Redis to function. If unavailable, sync operations and webhook processing will not run.

### 4. ANTHROPIC_API_KEY Required for Documentation Generation
AI-powered documentation generation skips silently if the API key is not set. Set `ANTHROPIC_API_KEY` and ensure `ANTHROPIC_TOKENS_PER_HOUR` budget is sufficient for your workload.

### 5. Email Notifications Not Implemented
The platform does not send email notifications (e.g., on org invites, doc generation completion). Notifications are logged but not delivered via SMTP.

### 6. SQLite Not Supported
The platform is PostgreSQL-only. SQLite driver compatibility is not tested or maintained.

### 7. Self-Hosted GitLab Requires Manual Configuration
For self-hosted GitLab instances, set `GITLAB_BASE_URL` to the API root (e.g., `https://gitlab.company.com/api/v4`). OAuth callback URLs must be explicitly registered in the GitLab application settings.

### 8. Webhook Payload Size Limit
Webhook payloads larger than the default 5 MB (coverage uploads) or 10 MB (GitHub pushes) are rejected. Configure your Git providers to send smaller payloads or split large reports across multiple uploads.

### 9. Coverage Format Auto-Detection
The `X-Coverage-Format` header must be provided explicitly (`go`, `lcov`, `cobertura`, or `jacoco`). The service does not attempt to auto-detect format from payload content.

### 10. Token Revocation Delay
Revoked access tokens remain valid until expiration (TTL window). Use short `ACCESS_TOKEN_TTL` values (e.g., 15 minutes) to minimize exposure window.

---

**For detailed development guidance, see `CLAUDE.md` and `GITHUB_SYNC_TESTING.md` in the repository root.**