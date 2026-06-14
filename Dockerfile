# syntax=docker/dockerfile:1
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
