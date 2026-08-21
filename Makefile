.PHONY: help build run dev test test-e2e test-e2e-live e2e-stack fake-gitlab lint clean docker-up docker-down docker-logs swagger

help:
	@echo "IDP Backend - Available commands"
	@echo ""
	@echo "Development:"
	@echo "  make dev              - Run server in development mode"
	@echo "  make build            - Build binary"
	@echo "  make run              - Run binary"
	@echo ""
	@echo "Testing:"
	@echo "  make test             - Run tests"
	@echo "  make test-coverage    - Run tests with coverage"
	@echo "  make test-e2e         - Run the end-to-end suite (needs docker)"
	@echo "  make test-e2e-live    - Run the opt-in tests against the real gitlab.com API"
	@echo "  make fake-gitlab      - Serve the fake GitLab API on its own"
	@echo "  make e2e-stack        - Hold the full E2E stack open (for the browser suite)"
	@echo "  make lint             - Run linter"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-up        - Start docker compose services"
	@echo "  make docker-down      - Stop docker compose services"
	@echo "  make docker-logs      - Show docker compose logs"
	@echo ""
	@echo "Utilities:"
	@echo "  make clean            - Clean build artifacts"
	@echo "  make fmt              - Format code"
	@echo "  make mod-tidy         - Tidy go.mod"
	@echo "  make swagger          - Generate Swagger/OpenAPI documentation"

# Development
dev: docker-up
	@echo "Starting IDP Backend in development mode..."
	@go run cmd/server/main.go

build:
	@echo "Building IDP Backend..."
	@go build -o bin/idp-server cmd/server/main.go
	@echo "Built successfully: bin/idp-server"

run: build
	@echo "Running IDP Backend..."
	@./bin/idp-server

# Testing
test:
	@echo "Running tests..."
	@go test ./... -v

test-coverage:
	@echo "Running tests with coverage..."
	@go test ./... -v -coverprofile=coverage.out
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# End to end: real server binary, real Postgres and Redis in throwaway
# containers, and a fake GitLab whose payloads were captured from gitlab.com.
# The suite manages its own containers and ports, so it will not touch the
# docker compose stack used for development.
test-e2e:
	@echo "Running end-to-end suite..."
	@go test -tags e2e -count=1 -timeout 20m -v ./e2e/...

# Hits the real gitlab.com anonymously to prove the recorded payload shapes are
# still what GitLab sends. Needs network access.
test-e2e-live:
	@echo "Running live provider tests against gitlab.com..."
	@GITLAB_LIVE_TEST=1 go test -count=1 -run Live -v ./internal/integrations/gitlab/

fake-gitlab:
	@go run ./cmd/fake-gitlab -addr 127.0.0.1:8081 -token glpat-e2e-token

# Holds the deterministic stack open (throwaway Postgres and Redis, fake GitLab,
# real server) for the browser suite and for manual poking.
e2e-stack:
	@./scripts/e2e-stack.sh

# Linting
lint:
	@echo "Running linter..."
	@golangci-lint run ./...

# Docker
docker-up:
	@echo "Starting docker compose services..."
	@docker compose up -d
	@echo "Services started. Waiting for health checks..."
	@sleep 5

docker-down:
	@echo "Stopping docker compose services..."
	@docker compose down

docker-logs:
	@docker compose logs -f

# Utilities
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@go clean
	@echo "Clean done"

fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@echo "Format done"

mod-tidy:
	@echo "Tidying go.mod..."
	@go mod tidy
	@echo "Tidy done"

swagger:
	@echo "Generating Swagger/OpenAPI documentation..."
	@go run github.com/swaggo/swag/cmd/swag@v1.8.12 init -g cmd/server/main.go -o docs --parseInternal --parseDependency
	@echo "Swagger documentation generated in docs/"
