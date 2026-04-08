.PHONY: run build test lint clean

# Build the application binary
build:
	go build -o metering-api ./cmd/server

# Run the application
run:
	go run ./cmd/server

# Run all tests with race detection
test:
	go test ./... -v -race -count=1

# Run linter
lint:
	golangci-lint run ./...

# Clean build artifacts and database
clean:
	rm -f metering-api
	rm -f metering.db
	rm -rf uploads/*
	touch uploads/.gitkeep
