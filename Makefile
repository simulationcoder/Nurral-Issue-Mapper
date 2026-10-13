.PHONY: web build test clean

# Build the page, then the binary that embeds it.
build: web
	go build -o bin/issue-mapper ./cmd/issue-mapper

web:
	cd web && npm ci && npm run build

test:
	go test ./...
	cd web && npm test

clean:
	rm -rf bin web/dist/assets
