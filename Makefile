.PHONY: build run test lint sast clean setup help

BINARY := bin/harden-repos
CMD := ./cmd/harden-repos
PIPELINES_DIR := .pipelines

help:
	@echo 'Targets:'
	@echo '  build   Build the harden-repos binary into bin/'
	@echo '  run     Run the harden-repos CLI (pass args via ARGS="...")'
	@echo '  test    Run unit tests with race detector'
	@echo '  lint    Run golangci-lint across the module'
	@echo '  sast    Run the full SAST suite from rios0rios0/pipelines'
	@echo '  clean   Remove build artifacts'
	@echo '  setup   Clone/update the rios0rios0/pipelines repository locally'

setup:
	@if [ ! -d "$(PIPELINES_DIR)" ]; then \
		git clone --depth 1 https://github.com/rios0rios0/pipelines.git "$(PIPELINES_DIR)"; \
	else \
		git -C "$(PIPELINES_DIR)" pull --ff-only; \
	fi

build:
	go build -o $(BINARY) $(CMD)

run:
	go run $(CMD) $(ARGS)

test:
	go test -race -tags=unit ./...

lint:
	golangci-lint run ./...

sast: setup
	bash $(PIPELINES_DIR)/scripts/sast/run-all.sh

clean:
	rm -rf bin coverage.out coverage.html
