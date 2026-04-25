.PHONY: help build run dev test lint clean docker-up docker-down docker-logs

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