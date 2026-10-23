.PHONY: build-api build-game build-worker build-migrate build-admin build-all run-api run-game run-worker run-admin test test-cover lint migrate migrate-status db-fresh db-fresh-down docker-up docker-down clean bench bench-integration loadtest

build-api:
	go build -o bin/api ./cmd/api

build-game:
	go build -o bin/game ./cmd/game

build-worker:
	go build -o bin/worker ./cmd/worker

build-migrate:
	go build -o bin/migrate ./cmd/migrate

build-admin:
	go build -o bin/admin ./cmd/admin

build-all: build-api build-game build-worker build-migrate build-admin

run-api:
	go run ./cmd/api

run-game:
	go run ./cmd/game

run-worker:
	go run ./cmd/worker

run-admin:
	go run ./cmd/admin

test:
	go test ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	go vet ./...

migrate:
	go run ./cmd/migrate

migrate-status:
	go run ./cmd/migrate -status

# A brand-new throwaway Postgres (container battlesquad_fresh_pg, port 5440; not the dev DB),
# migrated: schema, reference game data and, with ADMIN_BOOTSTRAP_EMAIL / _PASSWORD set, the
# first admin. Point the servers at it with POSTGRES_DSN=$(FRESH_DSN).
FRESH_PG_PORT ?= 5440
FRESH_DSN = postgres://postgres:postgres@localhost:$(FRESH_PG_PORT)/battlesquad?sslmode=disable

db-fresh:
	docker run -d --name battlesquad_fresh_pg -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=battlesquad -p $(FRESH_PG_PORT):5432 postgres:15-alpine
	until docker exec battlesquad_fresh_pg pg_isready -U postgres -d battlesquad >/dev/null 2>&1; do sleep 1; done
	sleep 2
	POSTGRES_DSN='$(FRESH_DSN)' go run ./cmd/migrate
	@echo "fresh DB ready: POSTGRES_DSN='$(FRESH_DSN)'"

db-fresh-down:
	docker rm -f battlesquad_fresh_pg

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

clean:
	rm -rf bin/ coverage.out coverage.html

bench:
	go test -bench=. -benchmem -count=3 -run='^$$' ./internal/game/match/...

bench-integration:
	go test -bench=. -benchmem -count=1 -tags=integration -run='^$$' ./internal/game/match/... ./internal/game/ws/...

loadtest:
	go run ./cmd/loadtest -players 100 -duration 60s
