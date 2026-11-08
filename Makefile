VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build test race lint fuzz bench schema fmt cross generate generate-check registry
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/gthok ./cmd/gthok
fmt:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then printf '%s\n' "$$out"; exit 1; fi
CROSS_DIR ?= dist/cross
cross:
	@set -e; for os in darwin linux; do for arch in amd64 arm64; do \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o "$(CROSS_DIR)/gthok_$${os}_$${arch}" ./cmd/gthok; \
	done; done
test:
	go test ./...
race:
	go test -race ./...
# The version CI pins (renovate updates ci.yml), built with the local Go.
GOLANGCI_LINT_VERSION = $(shell sed -n 's/^ *version: \(v2\.[0-9.]*\).*/\1/p' .github/workflows/ci.yml | head -1)
lint:
	@test -n "$(GOLANGCI_LINT_VERSION)" || { echo "lint: no golangci-lint version in .github/workflows/ci.yml" >&2; exit 1; }
	@# Build for the host; GOOS/GOARCH then only select what gets analysed.
	env -u GOOS -u GOARCH GOBIN="$(CURDIR)/bin/golangci-lint-$(GOLANGCI_LINT_VERSION)" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	"$(CURDIR)/bin/golangci-lint-$(GOLANGCI_LINT_VERSION)/golangci-lint" run ./...
# Go fuzzes one target per run. CI uses FUZZTIME=200000x.
# Name:pkg@N fixes N for filesystem-bound targets (~100-300 execs/s).
FUZZTIME ?= 30s
FUZZ_TARGETS := $(shell find . -type d -name .git -prune -o -type d -name worktrees -prune -o -type d -name dist -prune -o -type d -name vendor -prune -o \
	-type f -name '*_test.go' -exec grep -H '^func Fuzz' {} + | \
	awk -F: '{ name = $$2; sub(/^func /, "", name); sub(/[[:space:]]*[()].*/, "", name); pkg = $$1; sub(/\/[^\/]+$$/, "", pkg); print name ":" pkg }' | sort -u)
FUZZ_OVERRIDES := FuzzLayeredConfig@20000x FuzzTrustLifecycle@5000x FuzzTrustPathMatch@20000x

fuzz:
	@set -e; for t in $(FUZZ_TARGETS); do \
		pkg=$${t#*:}; time=$(FUZZTIME); \
		for override in $(FUZZ_OVERRIDES); do \
			case $$override in "$${t%%:*}"@*) time=$${override#*@};; esac; \
		done; \
		case $$pkg in *@*) time=$${pkg#*@}; pkg=$${pkg%@*};; esac; \
		echo "fuzz $${t%%:*}"; \
		go test -run '^$$' -fuzz "^$${t%%:*}$$" -fuzztime $$time $$pkg; \
	done
bench:
	go test -run '^$$' -bench . -benchmem ./...
schema:
	go generate ./internal/schema
generate: schema
	@if [ -n "$(GIT_SRC)" ]; then $(MAKE) registry; fi
generate-check: generate
	git diff --exit-code
registry:
	@test -n "$(GIT_SRC)" || { echo "registry: set GIT_SRC to the Git v2.56.0 source tree" >&2; exit 1; }
	go run ./internal/registry/gen -git-src "$(GIT_SRC)" -out internal/registry/data -verify
