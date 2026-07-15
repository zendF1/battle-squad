.PHONY: build-api build-game build-worker build-migrate build-all run-api run-game run-worker test test-cover lint migrate docker-up docker-down clean bench bench-integration loadtest

build-api:
	go build -o bin/api ./cmd/api

build-game:
	go build -o bin/game ./cmd/game

build-worker:
	go build -o bin/worker ./cmd/worker

build-migrate:
	go build -o bin/migrate ./cmd/migrate

build-all: build-api build-game build-worker build-migrate

run-api:
	go run ./cmd/api

run-game:
	go run ./cmd/game

run-worker:
	go run ./cmd/worker

test:
	go test ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	go vet ./...

migrate:
	go run ./cmd/migrate

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
