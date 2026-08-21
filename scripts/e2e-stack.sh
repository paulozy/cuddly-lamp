#!/usr/bin/env bash
# Boots the deterministic end-to-end stack and holds it open: throwaway
# Postgres and Redis, the fake GitLab API, and the real server.
#
# The Go suite (`make test-e2e`) builds the same stack in process and tears it
# down per run. This script exists for the browser suite, which needs something
# long-lived to point a frontend at, and for poking at the system by hand.
#
#   backend:      http://localhost:3000
#   fake gitlab:  http://localhost:8081
#   org token:    glpat-e2e-token   (paste this in Settings → GitLab)
#   projects:     gitlab.com/gitlab-org/nested-group/gitlab-runner
#                 gitlab.com/gitlab-org/huge-monorepo
#
# Ctrl-C tears everything down.
set -euo pipefail

cd "$(dirname "$0")/.."

SERVER_PORT="${SERVER_PORT:-3000}"
FAKE_PORT="${FAKE_PORT:-8081}"
PG_PORT="${PG_PORT:-55432}"
REDIS_PORT="${REDIS_PORT:-63799}"
FAKE_TOKEN="${FAKE_TOKEN:-glpat-e2e-token}"

PG_NAME="idp-e2e-stack-pg"
REDIS_NAME="idp-e2e-stack-redis"

# A port already in use almost always means the development stack is running.
# Failing loudly beats silently talking to the wrong database.
for port in "$SERVER_PORT" "$FAKE_PORT" "$PG_PORT" "$REDIS_PORT"; do
    if (echo >"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
        echo "port $port is already in use — stop whatever holds it (make docker-down?) or override the *_PORT variables" >&2
        exit 1
    fi
done

pids=()
cleanup() {
    echo
    echo "e2e-stack: tearing down..."
    for pid in "${pids[@]:-}"; do
        [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
    done
    docker stop "$PG_NAME" "$REDIS_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

echo "e2e-stack: starting postgres and redis..."
docker rm -f "$PG_NAME" "$REDIS_NAME" >/dev/null 2>&1 || true
docker run -d --rm --name "$PG_NAME" \
    -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=idp_e2e \
    -p "127.0.0.1:$PG_PORT:5432" postgres:15-alpine >/dev/null
docker run -d --rm --name "$REDIS_NAME" \
    -p "127.0.0.1:$REDIS_PORT:6379" redis:7-alpine >/dev/null

for _ in $(seq 1 60); do
    if docker exec "$PG_NAME" psql -U postgres -d idp_e2e -q -c 'SELECT 1' >/dev/null 2>&1; then
        break
    fi
    sleep 1
done
docker exec "$PG_NAME" psql -U postgres -d idp_e2e -q -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp";'

echo "e2e-stack: starting fake gitlab on :$FAKE_PORT..."
go run ./cmd/fake-gitlab -addr "127.0.0.1:$FAKE_PORT" -token "$FAKE_TOKEN" &
pids+=($!)

echo "e2e-stack: starting server on :$SERVER_PORT..."
# Every provider setting is passed explicitly so a stray .env cannot point this
# run at a real GitLab or a real database.
PORT="$SERVER_PORT" \
DB_HOST=127.0.0.1 DB_PORT="$PG_PORT" DB_USER=postgres DB_PASSWORD=postgres DB_NAME=idp_e2e DB_SSLMODE=disable \
REDIS_HOST=127.0.0.1 REDIS_PORT="$REDIS_PORT" \
JWT_SECRET=e2e-jwt-secret-e2e-jwt-secret-e2e \
ENCRYPTION_KEY="$(openssl rand -base64 32)" \
GITLAB_BASE_URL="http://127.0.0.1:$FAKE_PORT" \
GITHUB_TOKEN= GITLAB_TOKEN= ANTHROPIC_API_KEY= \
WEBHOOK_BASE_URL="http://[::1]:$SERVER_PORT" \
LOG_LEVEL=info \
    go run ./cmd/server &
pids+=($!)

for _ in $(seq 1 90); do
    if curl -fsS "http://127.0.0.1:$SERVER_PORT/api/v1/health" >/dev/null 2>&1; then
        echo
        echo "e2e-stack: ready."
        echo "  backend      http://localhost:$SERVER_PORT"
        echo "  fake gitlab  http://localhost:$FAKE_PORT"
        echo "  GitLab token to paste in Settings → GitLab: $FAKE_TOKEN"
        echo "  repository URL to add: https://gitlab.com/gitlab-org/nested-group/gitlab-runner"
        echo
        wait
    fi
    sleep 1
done

echo "e2e-stack: server never became healthy" >&2
exit 1
