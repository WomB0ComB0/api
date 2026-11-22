# Makefile for API Infrastructure (api.mikeodnis.dev)

COMPOSE := docker compose

.PHONY: help dev dev-obs prod stop clean logs test test-core test-media test-health build rebuild ps health db-backup db-restore db-shell scale-core scale-media install-core install-media lint-core lint-media format-media init

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

dev: ## Start development environment
	$(COMPOSE) up -d --build
	@echo "Development environment started"
	@echo "API: http://localhost/health"
	@echo "Traefik Dashboard: http://localhost:8080"

dev-obs: ## Start development environment with OpenTelemetry Collector
	$(COMPOSE) --profile observability up -d --build

prod: ## Start production environment
	$(COMPOSE) -f docker-compose.yml -f docker-compose.prod.yml up -d --remove-orphans

stop: ## Stop all services
	$(COMPOSE) down

clean: ## Stop and remove all containers, volumes, and networks
	$(COMPOSE) down -v --remove-orphans

logs: ## Tail logs from all services
	$(COMPOSE) logs -f --tail=100

logs-%: ## Tail logs from specific service (e.g., make logs-core-service)
	$(COMPOSE) logs -f --tail=100 $*

test-core: ## Run tests and typecheck for core service
	cd services/core && bun test && bun run typecheck

test-media: ## Run tests and vet for media service
	cd services/media && go vet ./... && go test -race -v ./...

test-health: ## Run tests for health aggregator
	cd services/health && python3 -m unittest discover -v

test: test-core test-media test-health ## Run all service tests

build: ## Build all service images
	$(COMPOSE) build

rebuild: ## Rebuild all service images without cache
	$(COMPOSE) build --no-cache

ps: ## Show running containers
	$(COMPOSE) ps

health: ## Check aggregate health of all services
	@curl -fsS http://localhost/health | jq '.'

db-backup: ## Backup PostgreSQL database
	@mkdir -p backups
	docker exec api-postgres pg_dump -U postgres api > backups/backup-$$(date +%Y%m%d-%H%M%S).sql
	@echo "Database backup created in backups/"

db-restore: ## Restore PostgreSQL database (usage: make db-restore FILE=backup.sql)
	@if [ -z "$(FILE)" ]; then \
		echo "Error: FILE parameter required. Usage: make db-restore FILE=backup.sql"; \
		exit 1; \
	fi
	cat backups/$(FILE) | docker exec -i api-postgres psql -U postgres api

db-shell: ## Open PostgreSQL shell
	docker exec -it api-postgres psql -U postgres api

scale-core: ## Scale core service (usage: make scale-core N=3)
	$(COMPOSE) up -d --scale core-service=$(N)

scale-media: ## Scale media service (usage: make scale-media N=2)
	$(COMPOSE) up -d --scale media-service=$(N)

install-core: ## Install dependencies for core service
	cd services/core && bun install

install-media: ## Download dependencies for media service
	cd services/media && go mod download

lint-core: ## Typecheck core service
	cd services/core && bun run typecheck

lint-media: ## Vet media service
	cd services/media && go vet ./...

format-media: ## Format media service code
	cd services/media && go fmt ./...

init: ## Initialize project (first-time setup)
	cp -n .env.example .env || true
	mkdir -p letsencrypt logs/traefik
	touch letsencrypt/acme.json && chmod 600 letsencrypt/acme.json
	@echo "Project initialized. Edit .env and run 'make dev'."
