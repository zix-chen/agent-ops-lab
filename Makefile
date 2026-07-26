.PHONY: run test check build

run:
	go run ./cmd/server

test:
	go test ./...

check:
	go fmt ./...
	go vet ./...
	go test -race ./...

build:
	CGO_ENABLED=0 go build -trimpath -o agent-ops-lab ./cmd/server
