# syntax=docker/dockerfile:1

# Standalone path for `docker build .`. This image compiles the web bundle
# and the Go binary inside the build. CI and releases do not use this file:
# they cross-compile on the runner and copy the binary with Dockerfile.goreleaser.
#
# TYPST_SHA256_* must be updated together with TYPST_VERSION.
# renovate: datasource=github-releases depName=typst/typst
ARG TYPST_VERSION=0.15.1
ARG TYPST_SHA256_AMD64=a6d077d0a95eed5a2eba715b2dae06be954f624ccbf85758a03f389ded33118c
ARG TYPST_SHA256_ARM64=5aa8d74a3d906e60ea12a66ac2f37f8eef1b14cbad7182a745e393a10c23dcee

FROM --platform=$BUILDPLATFORM node:24.21.0-bookworm-slim AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN corepack prepare pnpm@12.10.1 --activate && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27.2-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY sqlc.yaml ./
COPY web/embed.go ./web/embed.go
COPY --from=web /src/web/dist ./web/dist
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG REVISION=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${REVISION} -X main.date=${BUILD_DATE}" \
    -o /out/belegapp ./cmd/belegapp

FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS typst
ARG TYPST_VERSION
ARG TYPST_SHA256_AMD64
ARG TYPST_SHA256_ARM64
ARG TARGETARCH
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl xz-utils \
    && rm -rf /var/lib/apt/lists/* \
    && case "$TARGETARCH" in \
         amd64) arch=x86_64; sha="$TYPST_SHA256_AMD64" ;; \
         arm64) arch=aarch64; sha="$TYPST_SHA256_ARM64" ;; \
         *) echo "unsupported TARGETARCH=$TARGETARCH" >&2; exit 1 ;; \
       esac \
    && curl -fsSL -o /tmp/typst.tar.xz \
         "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-${arch}-unknown-linux-musl.tar.xz" \
    && echo "${sha}  /tmp/typst.tar.xz" | sha256sum -c - \
    && tar -xJf /tmp/typst.tar.xz -C /tmp \
    && install -m 0755 "/tmp/typst-${arch}-unknown-linux-musl/typst" /usr/local/bin/typst \
    && mkdir -p /data \
    && chown 65532:65532 /data \
    && chmod 0750 /data

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
ARG VERSION=dev
ARG REVISION=none
COPY --from=build /out/belegapp /belegapp
COPY --from=typst /usr/local/bin/typst /usr/local/bin/typst
COPY --from=typst --chown=65532:65532 /data /data
ENV PATH=/usr/local/bin:/usr/bin \
    BELEGAPP_TYPST_BIN=/usr/local/bin/typst
USER 65532:65532
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["/belegapp"]
CMD ["serve"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/belegapp", "healthcheck", "--timeout", "4s"]
LABEL org.opencontainers.image.source="https://github.com/BlackDark/vc-belegapp" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.title="vc-belegapp" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
