default:
    @just --list

# Run the server
run:
    go run ./cmd/metery serve --migrate

# Build the binary
build:
    go build -o metery ./cmd/metery

# Generate proto code
generate:
    buf generate

# Run database migrations
migrate:
    go run ./cmd/metery migrate

# Tidy dependencies
tidy:
    go mod tidy

# Lint proto files
lint:
    buf lint

# Clean build artifacts
clean:
    rm -f metery
    rm -f *.db *.db-journal *.db-wal *.db-shm