SHELL := /bin/bash

APP=scriptorum

.PHONY: build test run docker-build docker-run

build:
	go build -o bin/$(APP) ./cmd/scriptorum

test:
	go test ./...

run: build
	./bin/$(APP)

docker-build:
	docker build -t $(APP):dev .

docker-run: docker-build
	docker run --rm -p 8080:8080 -v $(PWD)/data:/data $(APP):dev
