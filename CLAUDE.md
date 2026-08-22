# Project: IDP with AI Backend

## Overview

Backend of an Internal Developer Platform (IDP). Provides JWT-based authentication, OAuth integration (GitHub/GitLab), a repository catalog with GitHub sync, read-only pull request browsing, spatial repository relationship mapping, CI coverage ingestion, and AI-generated project documentation delivered as GitHub PRs.

Documentation generation is the **only** LLM-backed feature. Code review, semantic search, and code scaffolding were AI-driven and have been removed (migration `023`) — do not reintroduce them without an explicit decision.

## Tech Stack

- **Language**: Go 1.21+
- **Framework**: Gin (HTTP routing & middleware)
- **Database**: PostgreSQL 14+
- **Cache**: Redis (optional) via `go-redis/v9` — `internal/storage/redis/`
- **Job Queue**: `asynq` (Redis-backed) — `internal/jobs/`
- **Testing**: Go test (standard library)
- **Deploy**: Docker Compose (local dev), Docker (production-ready)
- **ORM**: GORM v2
- **Auth**: JWT (golang-jwt/jwt v5)
- **Password Hashing**: Argon2 (golang.org/x/crypto/argon2)
- **Encryption**: AES-256-GCM (crypto/aes, crypto/cipher)
- **AI Integration**: Anthropic API (Claude) — documentation generation only

## Architecture

```
internal/
├── api/
│   ├── handlers/         # HTTP request handlers (auth, repository, relationships, webhook, pull requests, coverage, docs, org config)
│   ├── middleware/       # JWT auth, CORS, logging
│   ├── routes.go         # Route definitions
│   └── factories/        # Dependency injection setup
├── coverage/             # Coverage report parsers (go, lcov, cobertura, jacoco)
├── integrations/
│   ├── anthropic/        # Claude documentation prompts + org context builder
│   ├── scm/              # Provider-neutral types, capability interfaces, adapters, resolver
│   ├── github/           # GitHub API client + webhook HMAC validation + Contents/PR APIs
│   └── gitlab/           # GitLab REST v4 client + webhook token validation
├── models/               # GORM models (User, Repository, Token, WebhookConfig, etc.)
├── services/             # Business logic (AuthService, RepositoryService, RepositoryRelationshipService, SyncService)
├── workers/              # asynq task handlers (SyncWorker, WebhookProcessor, DocsWorker)
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
- **End-to-end**: `make test-e2e` (build tag `e2e`, package `e2e/`) drives the real
  server binary over HTTP against throwaway Postgres/Redis containers and
  `internal/testsupport/fakegitlab`, a fake GitLab serving payloads captured from
  gitlab.com. Provider behaviour belongs there, not in a mock: the fake's own
  contract tests keep it honest against the real client, and `make test-e2e-live`
  checks the recorded shapes against gitlab.com. Browser coverage is Playwright in
  the frontend repo, run against `make e2e-stack`.

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
3. **Repository management**: Complete — CRUD endpoints, enriched list with aggregated stats via LATERAL joins, GitHub and GitLab sync (branches, commits, PRs/MRs, languages, tree-based CI/test detection), WebhookConfig registration
4. **Webhook pipeline**: Complete — `POST /api/v1/webhooks/github/:repoID` (HMAC-SHA256 signature) and `POST /api/v1/webhooks/gitlab/:repoID` (constant-time `X-Gitlab-Token` comparison — GitLab sends a shared secret, not a signature), both normalized into the same `WebhookEventPayload` so `webhook_processor` is provider-agnostic. Idempotency uses the provider's delivery ID (`X-GitHub-Delivery` / `X-Gitlab-Event-UUID`), falling back to a key derived from the payload when GitLab sends none.
5. **Field-level encryption**: Complete — AES-256-GCM encryption for sensitive fields (OAuth tokens, webhook secrets), transparent GORM hooks, CLI migration tool
6. **API Documentation**: Complete — Swagger/OpenAPI 2.0 with swaggo/swag, committed docs at `/swagger/index.html`; `make swagger` uses pinned `swag@v1.8.12`
7. **Pull Request Browsing**: Complete — read-only `PullRequestHandler` proxying the repository's own host (GitHub pull requests or GitLab merge requests): list, detail, and changed files with patches. Routes and DTOs keep saying "pull request"; the neutral term "change request" is internal to `internal/integrations/scm`. No queue, no cache, no analysis.
8. **Auto-Generated Documentation**: Complete — `TypeGenerateDocs` / `TypeGenerateOrgDocs` workers, `doc_generations` JSONB storage, branch/file/change-request creation on GitHub or GitLab, in-app Markdown editing via `PATCH /docs/:id`, and org-wide docs built from `anthropic.OrgContextBuilder`
9. **Spatial Repository Navigation**: Complete — `repository_relationships` graph model, directed typed repo-to-repo edges, `GET /repositories/graph`, relationship CRUD endpoints, and legacy `repository_dependencies` backfill
10. **Enriched Repository List**: Complete — Optimized SQL with a LATERAL join fetches the newest coverage upload per repository in a single query, zero N+1 problem
11. **Configurable AI Output Language**: Complete — `OrganizationConfig.OutputLanguage` (BCP 47) drives a System prompt injected into every Claude documentation call. Validated with `golang.org/x/text/language`. Defaults to `"en"`.
12. **Coverage via CI Upload (`internal/coverage/`)**: Complete — CI runs upload coverage reports to `POST /api/v1/repositories/:id/coverage` authenticated with a `cov_*` Bearer token. Supported formats: `go` (Go cover via `golang.org/x/tools/cover`), `lcov`, `cobertura` (XML), `jacoco` (XML). Parser stack also returns per-file granularity. Uploads persist in `coverage_uploads` keyed by `(repo, commit_sha)` and surface in the enriched repository list. Tokens are revocable, scoped per repo, and shown only once on creation.
13. **AI Feature Removal**: Complete — migration `023` drops `code_analyses`, `code_templates`, `code_embeddings`, the analysis/embeddings columns on `repositories`, and the Voyage + PR-review columns on `organization_configs`.

## Known Issues & Constraints

- **Partial test coverage**: services and redis have tests; postgres integration tests and handlers have none yet
- **Token validation**: Allows tokens not found in DB for backward compatibility (tokens created before DB migration)
- **Soft deletes**: Using `*time.Time` for DeletedAt (nullable), not `gorm.DeletedAt`
- **Worker in-process**: asynq worker runs in the same binary as the HTTP server; split to `cmd/worker/` when independent scaling is needed
- **Timezone handling**: PostgreSQL TIMESTAMP (no timezone) requires explicit UTC conversion in Go — always use `.UTC()`
- **StringArray**: `models.StringArray` is a custom type for PostgreSQL `text[]` — use it instead of `[]string` on any GORM model field mapped to a `text[]` column
- **Repository relationships**: `repository_relationships` is the canonical graph model for spatial navigation. Do not add new map behavior to legacy `repository_dependencies` except compatibility/backfill. Relationships are directed, same-organization only, allow multiple edges between the same repositories, and use `kind` values `http`, `async`, `library`, `data`, `infra`, `manual`, `other`.
- **Providers go through `internal/integrations/scm`**: `SyncService`, `DocsWorker` and `PullRequestHandler` depend on `scm.Provider` (capability interfaces: `CatalogReader`, `ChangeRequestReader`, `ChangeRequestWriter`, `WebhookRegistrar`, `CloneAuthorizer`), never on a forge client directly. `scm.For(repoType, creds)` dispatches on the provider the repository **URL** resolves to. Honouring that dispatch is load-bearing: the project path is not unique across forges, so a GitLab path run through the GitHub client silently imports an unrelated project's data. GitHub and GitLab (gitlab.com) are implemented; Gitea still returns `ErrUnsupportedProvider`, recorded on `sync_status`/`sync_error`.
- **Provider credentials are per host**: a repository is only synced with its own host's token — `scm.For` returns `ErrMissingCredentials` rather than falling back to another provider's token. Organization tokens (`organization_configs.github_token` / `gitlab_token`) win over the platform-level `GITHUB_TOKEN` / `GITLAB_TOKEN` env vars.
- **GitLab specifics that are easy to get wrong**: merge requests are addressed by `iid`, not `id`; `/languages` returns percentages, not byte counts; the recursive tree is paginated with no `truncated` flag, so `gitlab.Client.GetTree` synthesizes one at a 30-page ceiling (a path missing from a truncated tree proves nothing — `internal/detect` treats it as `unknown`); `changes_count` is a string and can be `"1000+"`; per-file line counts do not exist and are counted from the diff body. Nested groups are real project paths, so `utils.ParseRepositoryURL` keeps every GitLab path segment (`group/subgroup/project`) while GitHub stays `owner/repo`.
- **Webhook registration on localhost**: skipped automatically when `WEBHOOK_BASE_URL` contains `localhost`/`127.0.0.1` — use ngrok for local webhook testing
- **Field-level encryption**: Encrypted fields require `ENCRYPTION_KEY` at startup; existing unencrypted data must be migrated using `cmd/migrate-encrypt/` tool; decryption happens transparently via GORM `AfterFind` hooks
- **Documentation generation**: Doc generation is asynchronous and requires Redis/asynq, an organization `ANTHROPIC_API_KEY`, and the token of the repository's own provider (`github_token` with contents/PR permissions, or `gitlab_token` with the `api` scope). Generated Markdown is delivered as a pull request on GitHub or a merge request on GitLab, and also stored in `doc_generations.content`. Clone credentials come from `scm.CloneAuthorizer` (`x-access-token` on GitHub, `oauth2` on GitLab).
- **Swagger docs**: Generated by `swag init` from annotations in handler code; regenerate with `make swagger` or `go run github.com/swaggo/swag/cmd/swag@v1.8.12 init -g cmd/server/main.go -o docs --parseInternal --parseDependency`
- **Organization config upsert**: `UpsertOrganizationConfig` lists its `ON CONFLICT DO UPDATE` columns by hand. Postgres rejects the whole statement for one unknown name — it referenced five columns migration `023` dropped, which broke every config update until the E2E suite caught it — and a column missing from that list silently fails to persist on an existing row. Adding a field to `OrganizationConfig` means adding it there too.
- **Swagger CLI**: `swag` is not required globally; `make swagger` invokes the pinned CLI through `go run`. In restricted sandboxes this can fail until network/module cache access is available.

## Authentication & Organization Notes

- **Authorization has two layers, both required**: `middleware.RequireRole(minRole)` gates the route on the caller's organization role (`viewer < developer < maintainer < admin`), and the service layer independently checks that the resource belongs to the caller's organization. Neither substitutes for the other — a maintainer of org A must still be refused org B's repositories.
- **Role map** (`internal/api/routes.go`): reads are open to any member; `developer` covers creating repositories, triggering syncs, relationship CRUD and doc generation; `maintainer` covers updating/deleting repositories and minting or revoking coverage tokens; `admin` is enforced inside the handlers for organization config and org-wide docs.
- **Adding a write route?** Mount a role gate on it. The middleware existed unmounted for a long time, which meant a `viewer` could delete any repository in the organization.

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

## Auto-Generated Documentation Notes

- **Generation flow**: `POST /api/v1/repositories/:id/docs/generate` creates a `DocGeneration` row with `pending` status, enqueues `TypeGenerateDocs`, and returns `202 Accepted` with the doc generation ID. The handler resolves the repository's provider up front, so a missing GitLab/GitHub token is a `503` at request time instead of a failed generation later.
- **Supported doc types**: `adr`, `architecture`, `service_doc`, and `guidelines`.
- **Worker flow**: `DocsWorker` clones the repository shallowly, builds context from the directory tree, key files, and recent commits/PRs, then asks Claude to generate Markdown for each requested type.
- **Delivery**: Generated docs are committed to a new branch (`docs/auto-generated-{timestamp}`) on the repository's own host and opened as a pull request (GitHub) or merge request (GitLab) against the requested/base branch.
- **Storage**: `doc_generations.content` stores generated Markdown as JSONB; PR URL/number, status, branch, token usage, and errors are stored on the same row. `PATCH /docs/:id` edits the stored Markdown in place.
- **Token budget**: The HTTP handler enforces the org's hourly Anthropic budget via `SumTokensUsedSince`, which sums `doc_generations.tokens_used` — doc generation is the only token consumer left. Worker token usage is written back to the same row.
- **Org-wide docs**: `POST /api/v1/organizations/docs/generate` builds on `anthropic.OrgContextBuilder`, an aggregated snapshot of repos, dominant stacks, relationships and existing per-repo docs. ADRs require a `template_id` from the static registry in `internal/docs/templates.go`.

## Coverage via CI Upload Notes

- **Source**: Coverage is uploaded by the repository's CI via `POST /api/v1/repositories/:id/coverage`. The backend never reads coverage from the clone, so projects can keep `coverage.out` / `lcov.info` / etc. gitignored as usual.
- **Auth**: `Authorization: Bearer cov_*` token. Tokens are scoped to a single repository, hashed (SHA-256) at rest, shown plaintext only once on creation, and revocable. Manage via `POST/GET/DELETE /api/v1/repositories/:id/coverage/tokens`.
- **Headers** (required): `X-Coverage-Format` (`go|lcov|cobertura|jacoco`) and `X-Commit-SHA`. `X-Coverage-Branch` is optional.
- **Body**: raw report bytes. `Content-Type: application/octet-stream`. Limit: 5 MB.
- **Response**: synchronous 200 with parsed numbers (`lines_covered`, `lines_total`, `percentage`, `status`). 401 on bad token, 415 on unsupported format, 413 on oversize, 422 on parse failure.
- **Storage**: `coverage_uploads` table (one row per upload, kept indefinitely; future TTL via cron). The most recent upload wins wherever a single value is needed.
- **List integration**: The enriched repository query (`postgres_repository.go`) LATERAL-joins the newest `coverage_uploads` row per repo into `EnrichedStats`. This is the only consumer — coverage is never recomputed server-side.
- **`has_coverage`**: False when the LATERAL join found no row. The DTO exposes it so the UI can say "not configured" instead of rendering a misleading red 0%. Do not collapse the two states.
- **Idempotency**: Multiple uploads for the same `(repo, sha)` are accepted and persisted; uploads are never mutated, the newest row simply wins. Multi-flag merge (Codecov-style) is out of scope v1.

## Spatial Repository Navigation Notes

- **Canonical model**: Use `RepositoryRelationship` / `repository_relationships` for repo-to-repo map edges. `repository_dependencies` remains legacy compatibility data.
- **Graph API**: `GET /api/v1/repositories/graph` returns all repositories in the authenticated organization as `nodes`, including independent repositories, plus matching relationship `edges`.
- **Relationship CRUD**: `POST /api/v1/repository-relationships`, `PATCH /api/v1/repository-relationships/:id`, and `DELETE /api/v1/repository-relationships/:id`.
- **Direction**: `source_repository_id -> target_repository_id`. For example, `web-checkout -> payments-api` for HTTP calls and `payments-api -> shared-libs` for library usage.
- **Kinds**: `http`, `async`, `library`, `data`, `infra`, `manual`, `other`. Store kind-specific details in JSONB `metadata` rather than adding columns prematurely.
- **Source/confidence**: Manual creates use `source=manual` and `confidence=1.0`. Future inference can use `analysis`, `manifest`, `import`, or `webhook` without changing the API shape.
- **Validation**: Reject self-relationships and relationships across organizations. Multiple relationships between the same two repos are valid when they represent distinct mechanisms.

## Swagger/OpenAPI Documentation

- **Library**: swaggo/swag v1.8.12 (code-first, annotation-based)
- **Format**: OpenAPI 2.0 (Swagger)
- **UI**: gin-swagger serving `/swagger/*any` route (Swagger UI embedded)
- **Generation**: `make swagger` or `go run github.com/swaggo/swag/cmd/swag@v1.8.12 init -g cmd/server/main.go -o docs --parseInternal --parseDependency`
- **Generated files**: `docs/docs.go` (committed), `docs/swagger.json` and `docs/swagger.yaml` (ignored in .gitignore)
- **Annotations**: Auth, repository, pull request, webhook, coverage, documentation, health, and Swagger UI routes are documented with `@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Security` markers
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
- `ANTHROPIC_API_KEY`, `GITHUB_TOKEN`, `GITLAB_TOKEN`: External API keys; Anthropic is used for documentation generation only. The provider tokens are platform-level fallbacks (organizations normally configure their own) used for webhook registration, PR/MR reads, private repository clones during doc generation, and documentation PR/MR creation.
- `ANTHROPIC_TOKENS_PER_HOUR`: Hourly token budget for doc generation (default `20000`)
- `WEBHOOK_BASE_URL`: Public base URL for webhook registration (e.g. ngrok URL); omit or use localhost to skip GitHub webhook registration
- `LOG_LEVEL`: Logging verbosity (info, debug, error)

## Do NOT

- Do not create documentation files unless asked
- Do not add dependencies without confirming first
- Do not change architecture without prior discussion
- Do not ignore failing tests (run before task completion)
- Do not over-engineer — solve the current problem, not hypothetical ones
- Do not reintroduce AI code review, semantic search, or AI code templates — they were deliberately removed
- Do not use weak password hashing (Argon2 is required)
- Do not mix UTC and local time — always be explicit with `.UTC()`
- Do not skip error handling at system boundaries (API input, DB, external services)

## Checkpoint — August 20, 2026

**Status**: ✅ Plain IDP — Auth + Repository Catalog + GitHub Sync + Webhook Pipeline + Encryption + Pull Request Browsing + Spatial Repository Graph + Coverage via CI Upload + AI Documentation Generation

### What this platform is now

A catalog-and-visibility IDP. It syncs repositories from GitHub, lets you browse
pull requests and their diffs, maps repo-to-repo relationships as a graph,
ingests coverage from CI, and generates project documentation with Claude.

### The AI removal (this session)

Everything that called an LLM except documentation generation was removed:

- **PR review findings** — `internal/ai` analysis types, the Anthropic analyzer,
  `analysis_worker`, `code_analyses`, and the GitHub review-posting wrapper.
  PR listing/detail/diff are untouched and now live in `handlers/pull_requests.go`.
- **Semantic search** — `internal/embeddings` (Voyage), the pgvector search
  query, the SSE synthesis stream and its Redis cache.
- **Code templates** — the `ai.Generator` interface, `template_worker`, and
  `code_templates`.
- **Auto-triggers** — initial analysis on repository creation, and analysis /
  embedding-indexing on push webhooks. A push now enqueues a sync and nothing else.

Three couplings had to be rewired rather than deleted:

1. `SumTokensUsedSince` summed `code_analyses` + `code_templates`. It now sums
   `doc_generations`, the only remaining token consumer.
2. The enriched repository query sourced coverage from `code_analyses.metrics`.
   It now LATERAL-joins `coverage_uploads`, which was the authoritative store all
   along. Quality score and analysis counts are gone from the DTO; `has_coverage`
   distinguishes "never measured" from "measured 0%".
3. `AnalysisHandler` mixed read-only PR endpoints with analysis triggers. It was
   split into a `PullRequestHandler` holding only the three read routes.

Migration `023` drops the schema: `code_analyses`, `code_templates`,
`code_embeddings`, the analysis/embeddings columns on `repositories`, and the
Voyage + PR-review columns on `organization_configs`. **It is destructive and
irreversible** — take a dump first if any generated findings, templates, or
vectors are worth keeping.

### Verification

`go build ./...`, `go vet ./...` and `go test ./...` all pass. Swagger was
regenerated; no AI routes remain in `docs/swagger.json`.

### Ready for next phase

Broader handler/integration test hardening and production key rotation.
