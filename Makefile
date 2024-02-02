
pk_file := private.key
proof_file := proof.ucan

HEAD_SHORT ?= $(shell git rev-parse --short HEAD)

GOVVV=go run github.com/ahmetb/govvv@v0.3.0 
GOVVV_FLAGS=$(shell $(GOVVV) -flags -pkg $(shell go list ./buildinfo))

# local run
run: 
	@HTTP_PORT=8081 \
	PRIVATEKEY=$(shell cat ${pk_file}) \
	PROOF=$(shell cat ${proof_file} | xxd -p | tr -d '\n') \
	go run .
.PHONY: run

# Lint
lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.51.0 run
.PHONY: lint

# Build 
build:
	go build -ldflags="${GOVVV_FLAGS}" -o api .
.PHONY: build

# Test
test: 
	go test ./... -race
.PHONY: test

build-images:
	docker build -t textile/basin_w3s:${HEAD_SHORT} .
	docker tag textile/basin_w3s:${HEAD_SHORT} textile/basin_w3s:latest
.PHONY: build-images

push-images:
	docker image push textile/basin_w3s:${HEAD_SHORT}
	docker image push textile/basin_w3s:latest
.PHONY: push-images
