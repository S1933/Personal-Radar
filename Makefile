# Personal Radar — developer and CI entry point.
#
# Targets:
#   make build        compile a binary for the current platform (fast dev loop)
#   make build-all    cross-compile linux/amd64, linux/arm64, darwin/amd64
#   make test         run the short test suite (CI default)
#   make test-all     run every test, including real network collectors
#   make vet          static checks
#   make tidy         go mod tidy
#   make run          build + run the service with the local config
#   make clean        remove build artifacts
#   make ci           build-all + vet + test (what the GitHub Action runs)

BIN       := bin/radar
DIST      := dist
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)
PACKAGE   := ./cmd/radar
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64

.PHONY: build build-all test test-all vet tidy run clean ci

build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PACKAGE)

# Cross-compile every platform the radar runs on. The Pi is arm64,
# the dev laptop is darwin/amd64, the CI runner is linux/amd64. The
# $(DIST) folder is gitignored; binaries are picked up by the release
# workflow, not pushed to the repo.
build-all:
	@mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d/ -f1); \
		arch=$$(echo $$platform | cut -d/ -f2); \
		out=$(DIST)/radar-$$os-$$arch; \
		echo ">> $$out"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			go build -ldflags "$(LDFLAGS)" -o $$out $(PACKAGE); \
	done

test:
	go test -short ./...

# Full suite — touches real network (X, Reddit, RSS). Disabled in CI
# by default; run this locally to validate sidecar integration.
test-all:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

run: build
	./$(BIN) run -config config/radar.yaml

clean:
	rm -rf bin $(DIST)

# Local-CI: the same commands the GitHub Action runs. Lets the
# developer catch a failing pipeline before pushing.
ci: vet test build-all
