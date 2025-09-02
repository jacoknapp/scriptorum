# syntax=docker/dockerfile:1
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod .
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/root/.cache/go-build go build -ldflags="-s -w" -o /out/scriptorum ./cmd/scriptorum

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app
COPY --from=build /out/scriptorum /app/scriptorum
COPY scriptorum.example.yaml /app/config.yaml
USER nonroot:nonroot
EXPOSE 8080
ENV CONFIG_PATH=/data/config.yaml ABS_DB_PATH=/data/scriptorum.db
VOLUME ["/data"]
ENTRYPOINT ["/app/scriptorum"]
