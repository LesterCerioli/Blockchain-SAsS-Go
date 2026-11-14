APP_NAME := payment-gateway
BIN_DIR := bin
GO := go

.PHONY: all build run test test-race vet fmt tidy clean migrate docker-build docker-up docker-down lint

all: fmt vet test build

build:
	$(GO) build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/$(APP_NAME) ./cmd

run:
	$(GO) run ./cmd serve

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

tidy:
	$(GO) mod tidy

lint:
	go vet ./...

migrate:
	$(GO) run ./cmd migrate

clean:
	rm -rf $(BIN_DIR) coverage.out

docker-build:
	docker build -t $(APP_NAME):local -f deployments/Dockerfile .

docker-up:
	docker compose -f deployments/docker-compose.yml up --build -d

docker-down:
	docker compose -f deployments/docker-compose.yml down -v
