PREFIX ?= /usr/local
MODULE  = github.com/philipcunningham/fizzle/pkg/version

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +%Y-%m-%d)

LDFLAGS = -ldflags "-X $(MODULE).Version=$(VERSION) -X $(MODULE).Commit=$(COMMIT) -X $(MODULE).Date=$(DATE)"

# Pinned version of the attribution tooling. Installed by `make tools` into
# $(GOPATH)/bin so CI can cache the results. `make build` does not invoke
# `go install` itself; a missing tool surfaces as a clear error from
# `make licenses`.
GO_LICENSES_VERSION := v1.6.0
GOLANGCI_LINT_VERSION := 2.13.1
GO_VERSION := 1.26.5
NODE_MAJOR := 26
VALE_VERSION := 3.19.0

LICENSES_DIR  := internal/licenses
LICENSES_FILE := $(LICENSES_DIR)/THIRD_PARTY_LICENSES.txt
PROJECT_LICENSE_EMBED := $(LICENSES_DIR)/LICENSE.txt

# Build tag wired into the release binaries so the embed in
# internal/licenses/licenses_release.go pulls in the generated attribution
# text. Without the tag the stub strings ship instead, which is what we
# want for `go test ./...` and other plain Go workflows.
RELEASE_TAGS := -tags release

# Shared release-build invocation for `build` and the platform targets.
GO_BUILD_RELEASE = CGO_ENABLED=0 go build $(LDFLAGS) $(RELEASE_TAGS)

build: licenses
	$(GO_BUILD_RELEASE) -o fizzle ./cmd/fizzle

license-tools:
	go install github.com/google/go-licenses@$(GO_LICENSES_VERSION)

tools: license-tools
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION)

tools-check:
	@go version | grep -q 'go$(GO_VERSION)' || \
	  (echo "Go $(GO_VERSION) required" >&2; exit 1)
	@node --version | grep -Eq '^v$(NODE_MAJOR)\\.' || \
	  (echo "Node $(NODE_MAJOR).x required" >&2; exit 1)
	@golangci-lint --version | grep -q 'version $(GOLANGCI_LINT_VERSION)' || \
	  (echo "golangci-lint $(GOLANGCI_LINT_VERSION) required" >&2; exit 1)
	@vale --version | grep -q '$(VALE_VERSION)' || \
	  (echo "vale $(VALE_VERSION) required" >&2; exit 1)

licenses: $(LICENSES_FILE) $(PROJECT_LICENSE_EMBED)

$(LICENSES_FILE): go.mod go.sum scripts/licenses.tmpl
	mkdir -p $(LICENSES_DIR)
	go-licenses report ./cmd/fizzle/... \
		--template scripts/licenses.tmpl \
		--ignore github.com/philipcunningham/fizzle \
		> $@
	go-licenses check ./cmd/fizzle/...

$(PROJECT_LICENSE_EMBED): LICENSE
	mkdir -p $(LICENSES_DIR)
	cp LICENSE $@

test:
	go test -race ./...

integration-test:
	go test -race -tags integration -count=1 -v ./pkg/integration/

fmt:
	go fmt ./...

# Check formatting without changing the working tree.
fmt-check:
	go run ./scripts/fmtcheck .

vet:
	go vet ./...

# `make lint` composes Go lint and prose lint.
lint: lint-go lint-docs

lint-go:
	golangci-lint run ./...

# Prose lint for markdown (vale). Hooks in .claude/settings.json run vale
# per edited file; this is the manual full-repo pass. Primary sources and
# per-file rule exceptions are configured in .vale.ini.
lint-docs:
	@command -v vale >/dev/null 2>&1 || \
	  (echo "vale not found on PATH. Install it with: brew install vale" >&2; exit 1)
	vale .

fuzz-seed:
	go test -run 'Fuzz' ./...

# Compile the real browser module and the dependencies that actually ship in
# it. CLI-only packages intentionally remain covered by their native builds.
wasm-check:
	GOOS=js GOARCH=wasm go build -o /dev/null ./web/wasm/module

# Build the browser core: the WASM module into the app's public assets
# and the Go runtime shim into the generated sources. Both outputs are
# gitignored; run this before `npm run dev` or `npm run smoke`.
wasm:
	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o web/app/src/core/fizzle.wasm ./web/wasm/module
	mkdir -p web/app/src/core/generated
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" web/app/src/core/generated/wasm_exec.js

# Web front end checks: format, lint, type check, unit tests, build,
# and the payload budget. Builds the WASM core first so the budget
# counts the real payload.
web-check: wasm
	@command -v npm >/dev/null 2>&1 || \
	  (echo "npm not found on PATH. Install Node $(NODE_MAJOR)+ to run web checks." >&2; exit 1)
	cd web/app && npm run check

# Exercise the production React bundle against the real WASM core.
web-smoke: wasm
	cd web/app && npm run smoke

# Regenerate the manual's screenshots from the production bundle. These
# are documentation rather than baselines, so nothing compares them and
# `check` doesn't run this. Regenerate when the surfaces they show move.
docshots: wasm
	cd web/app && npm run build && npm run docshots

web-fast: wasm
	@command -v npm >/dev/null 2>&1 || \
	  (echo "npm not found on PATH. Install Node $(NODE_MAJOR)+ to run web checks." >&2; exit 1)
	cd web/app && npm run check:fast

check-fast: fmt-check vet lint
	go test -short -race ./...
	$(MAKE) wasm-check web-fast

check-full: fmt-check vet lint test integration-test fuzz-seed wasm-check web-check web-smoke

check: check-full

coverage:
	go test -race -coverpkg=./... -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

benchmark:
	go test -run=^$$ -bench=. -benchmem ./...

profile:
	go test -run=^$$ -bench=BenchmarkConvertJUNGLISM$$ -benchmem -cpuprofile=cpu.prof -memprofile=mem.prof ./pkg/sfzconvert/
	@echo "CPU profile: cpu.prof (open with: go tool pprof -http=:9999 cpu.prof)"
	@echo "Mem profile: mem.prof (open with: go tool pprof -http=:9999 mem.prof)"

install: build
	mkdir -p $(PREFIX)/bin
	cp fizzle $(PREFIX)/bin/fizzle

clean:
	rm -f fizzle fizzle-linux-amd64 fizzle-darwin-amd64 fizzle-darwin-arm64 fizzle-windows-amd64.exe
	rm -f coverage.out coverage.html cpu.prof mem.prof
	rm -f $(LICENSES_FILE) $(PROJECT_LICENSE_EMBED)
	find . -type f -name '*.test' -not -path './.git/*' -delete

linux: licenses
	GOOS=linux GOARCH=amd64 $(GO_BUILD_RELEASE) -o fizzle-linux-amd64 ./cmd/fizzle

darwin-arm64: licenses
	GOOS=darwin GOARCH=arm64 $(GO_BUILD_RELEASE) -o fizzle-darwin-arm64 ./cmd/fizzle

darwin-amd64: licenses
	GOOS=darwin GOARCH=amd64 $(GO_BUILD_RELEASE) -o fizzle-darwin-amd64 ./cmd/fizzle

windows: licenses
	GOOS=windows GOARCH=amd64 $(GO_BUILD_RELEASE) -o fizzle-windows-amd64.exe ./cmd/fizzle

release: linux darwin-arm64 darwin-amd64 windows

# ----------------------------------------------------------------------------
# DEMO program
#
# Assembles testdata/assembly/DEMO.asm with nasm, then uses the fizzle CLI
# to write the binary onto a fresh FZ-1 disk image as a Type-5 "Program"
# file. nasm is not part of the standard toolchain; run `make asm-tools`
# to install it via Homebrew on macOS. The built DEMO.bin and DEMO.img
# are deterministic (nasm output) so committed copies and rebuilt copies
# match byte-for-byte.
# ----------------------------------------------------------------------------

DEMO_DIR := testdata/assembly
DEMO_ASM := $(DEMO_DIR)/DEMO.asm
DEMO_BIN := $(DEMO_DIR)/DEMO.bin
DEMO_IMG := $(DEMO_DIR)/DEMO.img

demo: $(DEMO_BIN) build
	rm -f $(DEMO_IMG)
	./fizzle disk new DEMO $(DEMO_IMG)
	./fizzle disk add $(DEMO_IMG) $(DEMO_BIN)
	./fizzle disk ls $(DEMO_IMG)

$(DEMO_BIN): $(DEMO_ASM)
	@command -v nasm >/dev/null 2>&1 || \
	  (echo "nasm not found on PATH. On macOS run: make asm-tools" >&2; exit 1)
	nasm -f bin $(DEMO_ASM) -o $@

# Install assembly toolchain (nasm) via Homebrew. Separate from `tools`
# because the standard build/test workflow does not require nasm.
asm-tools:
	@command -v brew >/dev/null 2>&1 || \
	  (echo "Homebrew required (see https://brew.sh)" >&2; exit 1)
	brew install nasm

.PHONY: build license-tools tools tools-check licenses test integration-test fuzz-seed fmt fmt-check vet lint lint-go lint-docs wasm wasm-check web-fast web-check web-smoke docshots check-fast check-full check coverage benchmark profile install clean linux darwin-arm64 darwin-amd64 windows release demo asm-tools
