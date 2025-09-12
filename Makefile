SHELL := /bin/bash

APP := scriptorum

# Detect Windows for .exe suffix
ifeq ($(OS),Windows_NT)
  EXE := .exe
else
  EXE :=
endif

OUT := bin/$(APP)$(EXE)

.PHONY: build test run docker-build docker-run

build:
	go build -o $(OUT) ./cmd/scriptorum

test:
	go test ./...

run: build
	$(OUT)

docker-build:
	docker build -t $(APP):dev .

docker-run: docker-build
	docker run --rm -p 8491:8491 -v $(PWD)/data:/data $(APP):dev
