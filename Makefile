# Makefile for API Infrastructure

.PHONY: help dev prod stop clean logs test build deploy

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

dev: ## Start development environment
	docker-compose up -d
	@echo "✅ Development environment started"
	@echo "📝 API: http://localhost/health"
	@echo "📊 Traefik Dashboard: http://localhost:8080"

prod: ## Start production environment
	docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d
	@echo "✅ Production environment started"

stop: ## Stop all services
	docker-compose down

clean: ## Stop and remove all containers, volumes, and networks
	docker-compose down -v --remove-orphans
	docker system prune -f

logs: ## Tail logs from all services
	docker-compose logs -f --tail=100

logs-%: ## Tail logs from specific service (e.g., make logs-core)
	docker-compose logs -f --tail=100 $*

test: ## Run tests for all services
	@echo "🧪 Testing Core Service..."
	cd services/core && npm test
	@echo "🧪 Testing Media Service..."
	cd services/media && go test -v ./...

build: ## Build all service images
	docker-compose build

rebuild: ## Rebuild all service images without cache
	docker-compose build --no-cache

ps: ## Show running containers
	docker-compose ps

health: ## Check health of all services
	@curl -s http://localhost/health | jq '.'

db-backup: ## Backup PostgreSQL database
	@mkdir -p backups
	docker exec api-postgres pg_dump -U postgres api > backups/backup-$$(date +%Y%m%d-%H%M%S).sql
	@echo "✅ Database backup created in backups/"

db-restore: ## Restore PostgreSQL database (usage: make db-restore FILE=backup-20240101.sql)
	@if [ -z "$(FILE)" ]; then \
		echo "❌ Error: FILE parameter required. Usage: make db-restore FILE=backup.sql"; \
		exit 1; \
	fi
	cat backups/$(FILE) | docker exec -i api-postgres psql -U postgres api
	@echo "✅ Database restored from $(FILE)"

db-shell: ## Open PostgreSQL shell
	docker exec -it api-postgres psql -U postgres api

hasura-console: ## Open Hasura console
	docker exec -it api-hasura hasura-cli console --admin-secret $${HASURA_GRAPHQL_ADMIN_SECRET}

hasura-migrate: ## Apply Hasura migrations
	docker exec -it api-hasura hasura-cli migrate apply

hasura-metadata: ## Apply Hasura metadata
	docker exec -it api-hasura hasura-cli metadata apply

scale-core: ## Scale core service (usage: make scale-core N=3)
	docker-compose up -d --scale core-service=$(N)

scale-media: ## Scale media service (usage: make scale-media N=2)
	docker-compose up -d --scale media-service=$(N)

update: ## Update all services
	git pull origin main
	docker-compose pull
	docker-compose up -d --remove-orphans
	docker system prune -f
	@echo "✅ Services updated"

install-core: ## Install dependencies for core service
	cd services/core && npm install

install-media: ## Install dependencies for media service
	cd services/media && go mod download

lint-core: ## Lint core service
	cd services/core && npm run lint

lint-media: ## Lint media service
	cd services/media && go vet ./...

format-core: ## Format core service code
	cd services/core && npm run format

format-media: ## Format media service code
	cd services/media && go fmt ./...

cert-renew: ## Renew SSL certificates
	docker-compose restart traefik
	@echo "✅ SSL certificates will be renewed automatically"

monitor: ## Open monitoring dashboard
	@echo "📊 BetterStack: https://betterstack.com"
	@echo "📈 Prometheus: http://localhost:9090 (if configured)"
	@echo "🔍 Traefik: http://localhost:8080"

init: ## Initialize project (first-time setup)
	@echo "🚀 Initializing API infrastructure..."
	cp .env.example .env
	@echo "📝 Edit .env with your configuration"
	mkdir -p letsencrypt logs
	chmod 600 letsencrypt/acme.json 2>/dev/null || touch letsencrypt/acme.json && chmod 600 letsencrypt/acme.json
	@echo "✅ Project initialized. Run 'make dev' to start."
