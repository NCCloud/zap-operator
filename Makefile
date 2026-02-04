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

## Tool Binaries
CONTROLLER_GEN ?= go run sigs.k8s.io/controller-tools/cmd/controller-gen@latest

.PHONY: manifests
manifests: ## Generate CRD manifests
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:dir=config/crd/bases

.PHONY: generate
generate: ## Generate code (DeepCopy, etc.)
	$(CONTROLLER_GEN) object paths="./api/..."
