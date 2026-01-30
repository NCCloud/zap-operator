SHELL := /bin/bash

IMG ?= zap-operator:latest

.PHONY: build
build:
	go build -o bin/manager ./cmd

.PHONY: run
run:
	go run ./cmd

.PHONY: test
test:
	go test ./...

.PHONY: docker-build
docker-build:
	docker build -t $(IMG) .
