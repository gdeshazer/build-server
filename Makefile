.PHONY: run build test clean help

run:
	go run . -config config.yaml

build:
	go build -o build-server .

test:
	go test ./...

clean:
	rm -f build-server build-server.db

help:
	@echo "Available targets:"
	@echo "  run    - Run the server with config.yaml"
	@echo "  build  - Build the binary (./build-server)"
	@echo "  test   - Run all tests"
	@echo "  clean  - Remove built binary and database"
	@echo "  help   - Show this help message"
