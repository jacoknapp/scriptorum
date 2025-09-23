# syntax=docker/dockerfile:1
ARG PREBUILT=false
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod .
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
# If PREBUILT is true and ./bin/scriptorum exists in the build context, use it; otherwise build from source.
RUN --mount=type=cache,target=/root/.cache/go-build if [ "${PREBUILT}" = "true" ] && [ -f ./bin/scriptorum ]; then \
			mkdir -p /out && cp ./bin/scriptorum /out/scriptorum; \
		else \
			go build -v -o /out/scriptorum ./cmd/scriptorum; \
		fi

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates curl && rm -rf /var/lib/apt/lists/*
WORKDIR /app
# copy built binary from build stage
COPY --from=build /out/scriptorum /app/scriptorum
# ensure default config is available under /data (create via COPY so no shell is required in distroless)
COPY scriptorum.example.yaml /data/scriptorum.yaml
RUN useradd -r -s /bin/false scriptorum && chown -R scriptorum:scriptorum /data /app
USER scriptorum
EXPOSE 8080
# Export env vars that the application reads at startup
ENV SCRIPTORUM_CONFIG_PATH=/data/scriptorum.yaml
ENV SCRIPTORUM_DB_PATH=/data/scriptorum.db
VOLUME ["/data"]
ENTRYPOINT ["/app/scriptorum"]
