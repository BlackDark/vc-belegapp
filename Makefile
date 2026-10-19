.PHONY: web web-build test lint sqlc vuln build docker docker-prebuilt screenshots

# web/dist is a build artifact, not a source file. web/embed.go embeds it with
# //go:embed, so it has to exist before anything compiles the web package:
# every Go target below depends on this one.
web-build:
	pnpm -C web install --frozen-lockfile
	pnpm -C web build

web: web-build
	pnpm -C web exec biome ci .
	pnpm -C web exec tsc --noEmit
	pnpm -C web exec vitest run

test: web-build
	go test -race -count=1 -coverprofile=coverage.out ./...

lint: web-build
	gofmt -l .
	golangci-lint run --timeout=5m
	pnpm -C web exec biome ci .

sqlc:
	go generate ./internal/db
	go tool sqlc vet
	./scripts/sqlc-diff.sh

vuln: web-build
	go tool govulncheck ./...

build: web
	CGO_ENABLED=0 go build -trimpath -o bin/belegapp ./cmd/belegapp

docker:
	docker build -t vc-belegapp:dev .

# Same layout CI uses: one native binary, then a copy into distroless.
docker-prebuilt: web-build
	mkdir -p dist/linux/amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/linux/amd64/belegapp ./cmd/belegapp
	docker build -f Dockerfile.goreleaser -t vc-belegapp:dev dist

# Landing-page images. Needs Typst (installed into ./bin when missing), poppler
# (pdftoppm), and python3-pil. Playwright downloads Chromium on first run.
screenshots: web-build
	@if ! command -v typst >/dev/null 2>&1; then ./scripts/install-typst.sh "$(CURDIR)/bin"; fi
	CGO_ENABLED=0 go build -trimpath -o bin/belegapp ./cmd/belegapp
	CGO_ENABLED=0 go build -trimpath -o bin/fakellm ./e2e/fakellm
	pnpm -C e2e install --frozen-lockfile
	pnpm -C e2e exec playwright install chromium
	PATH="$(CURDIR)/bin:$$PATH" BELEGAPP_BIN="$(CURDIR)/bin/belegapp" FAKE_LLM_BIN="$(CURDIR)/bin/fakellm" pnpm -C e2e exec playwright test screenshots.spec.ts
	python3 scripts/optimize-screenshots.py
