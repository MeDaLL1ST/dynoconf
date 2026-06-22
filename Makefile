.PHONY: help build test proto frontend docker up down run-client tidy

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n",$$1,$$2}'

build: ## Build the server binary (embeds web/dist)
	go build -o bin/dynoconf ./cmd/server

test: ## Run all tests (DB-backed tests use Docker/testcontainers)
	go test ./...

proto: ## Regenerate Go code from proto/config.proto
	protoc --go_out=. --go_opt=module=github.com/dynoconf/dynoconf \
	       --go-grpc_out=. --go-grpc_opt=module=github.com/dynoconf/dynoconf \
	       proto/config.proto

frontend: ## Build the frontend bundle into web/dist
	cd web && npm install && npm run build

docker: ## Build the single Docker image
	docker build -t dynoconf:latest .

up: ## Start the local stack (Postgres + service)
	docker compose up --build

down: ## Stop the local stack and remove volumes
	docker compose down -v

run-client: ## Run the reference client (needs CONFIG_SERVICE_KEY)
	go run ./examples/go-client

tidy: ## go mod tidy
	go mod tidy
