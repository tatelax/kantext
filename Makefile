.PHONY: build build-mcp build-all run run-web run-mcp test clean

# Build web server
build:
	go build -o bin/kantext ./cmd

# Build MCP server
build-mcp:
	go build -o bin/kantext-mcp ./cmd/mcp

# Build all
build-all: build build-mcp

# Run web server only
run-web:
	go run ./cmd

# Run MCP server only
run-mcp:
	go run ./cmd/mcp

# Run web server
run:
	@echo "Starting web server..."
	@go run ./cmd

# Run tests
test:
	go test -v ./tests/...

# Clean build artifacts
clean:
	rm -rf bin/
