.PHONY: all test build seed verify demo-traffic demo-failure

all: test build

test:
	go test -v ./...

build:
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker
	go build -o bin/seed ./cmd/seed
	go build -o bin/verify ./cmd/verify

seed:
	go run ./cmd/seed -n 1000 -out loadtest/tokens.json

verify:
	go run ./cmd/verify

demo-traffic:
	bash scripts/demo_traffic.sh

demo-failure:
	bash scripts/demo_failure.sh
