# syntax=docker/dockerfile:1
# Compile the Tailwind stylesheet from the templates so the image always ships
# CSS that matches the current UI, regardless of whether the committed
# internal/httpapi/web/static/css/tailwind.css copy is up to date. Runs on the
# native build platform (CSS is platform-independent) to avoid QEMU emulation.
FROM --platform=$BUILDPLATFORM node:20-bookworm-slim AS css
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
# Copy only what Tailwind needs: its config, the input stylesheet, and the
# templates it scans for utility classes (see `content` globs in the config).
COPY tailwind.config.js ./
COPY assets ./assets
COPY internal/httpapi/web ./internal/httpapi/web
RUN npm run build:css

# Build on the native build platform and cross-compile to the requested target
# platform so multi-arch images (linux/amd64, linux/arm64) each get a binary
# compiled for their own architecture.
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Overwrite the committed stylesheet with the freshly compiled one before the Go
# build embeds web/static/* via go:embed. This makes the CSS build a permanent
# part of producing the image — no manual `npm run build:css` step required.
COPY --from=css /src/internal/httpapi/web/static/css/tailwind.css ./internal/httpapi/web/static/css/tailwind.css
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -v \
    -ldflags "-X gitea.knapp/jacoknapp/scriptorum/internal/httpapi.Version=${VERSION}" \
    -o /out/scriptorum ./cmd/scriptorum

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates curl && rm -rf /var/lib/apt/lists/*
WORKDIR /app
# copy built binary from build stage
COPY --from=build /out/scriptorum /app/scriptorum
# ensure default config is available under /data (create via COPY so no shell is required in distroless)
COPY scriptorum.example.yaml /data/scriptorum.yaml
RUN useradd -r -s /bin/false scriptorum && chown -R scriptorum:scriptorum /data /app
USER scriptorum
EXPOSE 8491
# Export env vars that the application reads at startup
ENV SCRIPTORUM_CONFIG_PATH=/data/scriptorum.yaml
ENV SCRIPTORUM_DB_PATH=/data/scriptorum.db
VOLUME ["/data"]
ENTRYPOINT ["/app/scriptorum"]
