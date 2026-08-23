# Contributing to IDP Backend

Thank you for your interest in contributing to the IDP Backend project! This document provides guidelines for coding style, branch naming, PR process, and testing requirements.

## Getting Started

1. **Fork the repository** and clone it locally
2. **Set up your environment** following the Quick Start section in [README.md](README.md)
3. **Create a feature branch** from `main` using the naming convention below

## Branch Naming Convention

Use prefixed branch names to organize work:

- `feat/description` — New features (e.g., `feat/issues-and-contributors`)
- `fix/description` — Bug fixes (e.g., `fix/provider-error-classification`)
- `chore/description` — Maintenance, dependency updates, tooling
- `docs/description` — Documentation only
- `test/description` — Test coverage improvements
- `perf/description` — Performance optimizations
- `refactor/description` — Code refactoring without behavior changes

Use lowercase letters, hyphens to separate words, and descriptive names. Keep branch names concise and meaningful.

## Commit Message Format

Follow conventional commits for clarity and automated changelog generation:

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Type** (required):
- `feat` — A new feature
- `fix` — A bug fix
- `docs` — Documentation updates
- `style` — Code style changes (formatting, missing semicolons, etc.)
- `refactor` — Code refactoring without feature or behavior changes
- `perf` — Performance improvements
- `test` — Adding or updating tests
- `chore` — Dependency updates, build config, CI changes

**Scope** (optional): The affected component (e.g., `pull-requests`, `teams`, `sync`, `auth`, `api`)

**Subject** (required):
- Use imperative mood: "add" not "added" or "adds"
- Do not capitalize the first letter
- Do not end with a period
- Limit to 50 characters

**Body** (optional):
- Explain *what* and *why*, not *how*
- Wrap at 72 characters
- Separate from the subject with a blank line

**Footer** (optional):
- Reference issues: `Closes #123` or `Fixes #456`
- Note breaking changes: `BREAKING CHANGE: description`

**Example:**
```
feat(pull-requests): browse issues and contributors

Add endpoints to list pull requests with linked issues and contributor
metadata. Fetch data from the provider's API and cache results to
optimize repeated requests.

Closes #89
```

## Coding Style

### Go Code

- **Format**: Run `make fmt` before committing to ensure consistent formatting
- **Linting**: Run `make lint` and fix all warnings; CI will enforce this
- **Line length**: Aim for ≤100 characters; Go allows longer lines when semantically grouped
- **Comments**: Use clear, actionable comments; avoid redundant comments that repeat the code
- **Naming**:
  - Use `camelCase` for variables and functions
  - Use `PascalCase` for exported types and functions
  - Use descriptive names; avoid abbreviations unless standard (e.g., `ctx`, `repo`, `org`)
  - Interface names typically end in `-er` (e.g., `Enqueuer`, `DocumentationGenerator`)

- **Error handling**:
  ```go
  if err != nil {
      // Log with structured logging (zap)
      logger.Error("operation failed", zap.Error(err))
      // Return error with context if needed
      return fmt.Errorf("context: %w", err)
  }
  ```

- **Logging**: Use `go.uber.org/zap` for structured logging throughout the codebase
  ```go
  logger.Info("user registered", 
      zap.String("email", user.Email),
      zap.String("org_id", user.OrganizationID),
  )
  ```

- **Interfaces**: Keep them small and focused; define them near usage
- **Packages**: Organize by domain/feature, not by type (e.g., `oauth/`, `repositories/`, not `models/`, `handlers/`)

### SQL Migrations

- **File naming**: Use zero-padded sequence numbers (e.g., `001-init-schema.sql`, `025-add-organization-invites.sql`)
- **Idempotency**: Each migration should be runnable multiple times without error (use `IF NOT EXISTS`, `IF EXISTS`)
- **Transactions**: Wrap schema changes in explicit transactions when needed
- **Indexes**: Include indexes in migration files; document why each index exists
- **Constraints**: Explicitly define constraints (PRIMARY KEY, FOREIGN KEY, UNIQUE, NOT NULL)

## Pull Request Process

### Before Opening a PR

1. **Sync with main**: Ensure your branch is up-to-date
   ```bash
   git fetch origin
   git rebase origin/main
   ```

2. **Run tests locally**:
   ```bash
   make test          # Unit tests
   make test-coverage # With coverage report
   make lint          # Linting
   make fmt           # Code formatting
   ```

3. **Test your changes end-to-end** (if touching core features):
   ```bash
   make test-e2e      # Full E2E suite
   ```

### Opening the PR

1. **Title**: Use the same conventional commit format as your commits
   - Example: `feat(repositories): add enriched list with latest coverage`

2. **Description**: Include:
   - What problem does this solve?
   - How does it solve it?
   - Any breaking changes?
   - Screenshots or curl examples (for API changes)
   - Related issues: `Closes #123`

3. **Example PR template**:
   ```markdown
   ## Description
   Adds refresh token rotation and reuse detection to prevent hijacking.

   ## Changes
   - [ ] New `refresh_token_families` table to track token lineage
   - [ ] Reuse detection logic in `RefreshTokenService`
   - [ ] Family revocation on suspicious activity

   ## Testing
   - [ ] Unit tests for token rotation logic
   - [ ] E2E test for hijacking scenario

   ## Checklist
   - [x] Passing tests
   - [x] Updated documentation
   - [x] No breaking changes
   ```

### Review Expectations

- **Response time**: Maintainers aim to review within 2 business days
- **Feedback**: Be open to suggestions; we're iterating together
- **Changes requested**: Push additional commits; do not rebase until approved (easier review history)
- **Approval**: Requires at least one approval from a maintainer

## Testing Requirements

### Unit Tests

- Write tests for new functions and packages
- Aim for ≥70% code coverage on critical paths (auth, repositories, webhooks)
- Use table-driven tests for multiple scenarios

**Example:**
```go
func TestRefreshTokenRotation(t *testing.T) {
    tests := []struct {
        name      string
        setup     func(*testing.T) (*User, string)
        expectErr bool
    }{
        {
            name: "valid rotation succeeds",
            setup: func(t *testing.T) (*User, string) {
                user := &User{ID: uuid.New()}
                token := issueRefreshToken(user)
                return user, token
            },
            expectErr: false,
        },
        {
            name: "reused token detected",
            setup: func(t *testing.T) (*User, string) {
                user := &User{ID: uuid.New()}
                token := issueRefreshToken(user)
                rotateToken(token) // First rotation
                return user, token // Reuse old token
            },
            expectErr: true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            user, token := tt.setup(t)
            _, err := rotateToken(token)
            if (err != nil) != tt.expectErr {
                t.Errorf("unexpected error: %v", err)
            }
        })
    }
}
```

### Integration Tests

- Test database interactions with a real PostgreSQL instance (provided by docker-compose)
- Use transactions to isolate test data

### End-to-End Tests

- Required for features affecting the full request/response cycle
- Place in `e2e/` directory with `*_test.go` suffix
- Run with `make test-e2e`
- Use the deterministic stack (`e2e-stack.sh`) for manual testing

**Example:**
```go
// File: e2e/registration_test.go
func TestRegisterAndLogin(t *testing.T) {
    resp, err := http.Post("http://localhost:3000/api/v1/auth/register", ...)
    assert.NoError(t, err)
    assert.Equal(t, http.StatusCreated, resp.StatusCode)
}
```

## Review Checklist

Before submitting your PR, verify:

- [ ] **Coding Style**
  - [ ] Code formatted with `make fmt`
  - [ ] Linting passes with `make lint`
  - [ ] Comments are clear and meaningful
  - [ ] No debug print statements or commented-out code

- [ ] **Commits**
  - [ ] Commit messages follow conventional commits format
  - [ ] Each commit is logically self-contained
  - [ ] No accidental commits (e.g., `.env`, `bin/`, test artifacts)

- [ ] **Testing**
  - [ ] Unit tests added/updated for new code
  - [ ] All tests pass locally (`make test`)
  - [ ] Coverage report shows adequate coverage
  - [ ] E2E tests pass if feature touches API routes (`make test-e2e`)

- [ ] **Documentation**
  - [ ] Swagger/OpenAPI annotations updated (if API change)
  - [ ] README updated (if user-facing feature)
  - [ ] CLAUDE.md updated (if architecture change)
  - [ ] Database migration file created (if schema change)

- [ ] **Security & Compliance**
  - [ ] Sensitive data (tokens, keys) not logged or exposed in errors
  - [ ] Encryption used for sensitive database fields
  - [ ] CORS and CSRF protections in place (if web-facing)
  - [ ] SQL queries use parameterized statements (never string concatenation)
  - [ ] No hardcoded secrets or credentials

- [ ] **Database**
  - [ ] Migration file created and numbered sequentially
  - [ ] Migration is idempotent (can re-run safely)
  - [ ] Indexes defined for performance-critical queries
  - [ ] Foreign keys and constraints properly defined

- [ ] **API Changes**
  - [ ] Request/response types properly documented
  - [ ] Error responses documented (400, 401, 403, 404, 500)
  - [ ] Endpoint secured with appropriate auth checks
  - [ ] Rate limiting considered (if relevant)
  - [ ] Example curl commands provided in PR description

- [ ] **Performance**
  - [ ] No N+1 queries (use JOINs or LATERAL joins)
  - [ ] Background jobs used for long-running operations
  - [ ] Caching utilized where appropriate
  - [ ] Indexes prevent full table scans

- [ ] **Breaking Changes**
  - [ ] No breaking changes unless documented in footer
  - [ ] Deprecated endpoints kept for backward compatibility
  - [ ] Database migrations are additive (no dropping columns)

## Development Tools

### Essential Commands

```bash
make help          # Show all available commands
make dev           # Start server in development mode
make build         # Compile binary
make test          # Run unit tests
make test-coverage # Run tests with coverage report
make lint          # Run linter
make fmt           # Format code
make docker-up     # Start PostgreSQL and Redis
make docker-down   # Stop docker services
make swagger       # Generate OpenAPI documentation
```

### Environment Setup

Copy and configure `.env`:
```bash
cp .env.example .env
# Edit .env with local credentials and tokens
```

### Useful Debugging

- **View logs**: `make docker-logs`
- **Database access**: `docker exec -it idp-postgres psql -U postgres -d idp_dev`
- **Redis inspection**: `docker exec -it idp-redis redis-cli`
- **Run fake GitLab**: `make fake-gitlab` (for testing without network)
- **Hold E2E stack**: `make e2e-stack` (for manual testing in browser)

## Questions or Need Help?

- Check [CLAUDE.md](CLAUDE.md) for architecture and design decisions
- Review recent commits and PRs for patterns
- Open a discussion issue before starting large changes
- Reach out to maintainers in the PR review

Happy coding! 🚀