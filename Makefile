.PHONY: run curate test build

run:
	go run ./cmd/server

curate:
	go run ./cmd/curate -n 200

test:
	go vet ./...
	go test ./...

build:
	go build -o bin/wherever ./cmd/server
	go build -o bin/curate ./cmd/curate
