.PHONY: run build test clean

run:
	go run . -config config.yaml

build:
	go build -o build-server .

test:
	go test ./...

clean:
	rm -f build-server build-server.db
