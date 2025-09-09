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

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app
# copy built binary from build stage
COPY --from=build /out/scriptorum /app/scriptorum
# ensure default config is available under /data (create via COPY so no shell is required in distroless)
COPY --chown=nonroot:nonroot scriptorum.example.yaml /data/config.yaml
USER nonroot:nonroot
EXPOSE 8080
ENV CONFIG_PATH=/data/config.yaml
VOLUME ["/data"]
ENTRYPOINT ["/app/scriptorum"]
