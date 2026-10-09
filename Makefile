.PHONY: web test lint sqlc vuln build docker docker-prebuilt

web:
	pnpm -C web install --frozen-lockfile
	pnpm -C web exec biome ci .
	pnpm -C web exec tsc --noEmit
	pnpm -C web exec vitest run
	pnpm -C web build

test:
	go test -race -count=1 -coverprofile=coverage.out ./...

lint:
	gofmt -l .
	golangci-lint run --timeout=5m
	pnpm -C web exec biome ci .

sqlc:
	go generate ./internal/db
	go tool sqlc vet
	./scripts/sqlc-diff.sh

vuln:
	go tool govulncheck ./...

build: web
	CGO_ENABLED=0 go build -trimpath -o bin/belegapp ./cmd/belegapp

docker:
	docker build -t vc-belegapp:dev .

# Same layout CI uses: one native binary, then a copy into distroless.
docker-prebuilt:
	mkdir -p dist/linux/amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/linux/amd64/belegapp ./cmd/belegapp
	docker build -f Dockerfile.goreleaser -t vc-belegapp:dev dist
