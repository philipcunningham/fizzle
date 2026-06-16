PREFIX ?= /usr/local
MODULE  = github.com/philipcunningham/fizzle/pkg/version

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +%Y-%m-%d)

LDFLAGS = -ldflags "-X $(MODULE).Version=$(VERSION) -X $(MODULE).Commit=$(COMMIT) -X $(MODULE).Date=$(DATE)"

# Pinned versions of supply-chain tooling. Installed by `make tools` into
# $(GOPATH)/bin so CI can cache the results. `make build` does not invoke
# `go install` itself; missing tools surface as a clear error from
# `make licenses` or `make sbom`.
GO_LICENSES_VERSION    := v1.6.0
CYCLONEDX_GOMOD_VERSION := v1.7.0

LICENSES_DIR  := internal/licenses
LICENSES_FILE := $(LICENSES_DIR)/THIRD_PARTY_LICENSES.txt
PROJECT_LICENSE_EMBED := $(LICENSES_DIR)/LICENSE.txt
SBOM_FILE := fizzle.cdx.json
SBOM_EMBED := $(LICENSES_DIR)/sbom.cdx.json

# Build tag wired into the release binaries so the embed in
# internal/licenses/licenses_release.go pulls in the generated attribution
# text. Without the tag the stub strings ship instead, which is what we
# want for `go test ./...` and other plain Go workflows.
RELEASE_TAGS := -tags release

build: licenses sbom
	CGO_ENABLED=0 go build $(LDFLAGS) $(RELEASE_TAGS) -o fizzle ./cmd/fizzle

tools:
	go install github.com/google/go-licenses@$(GO_LICENSES_VERSION)
	go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@$(CYCLONEDX_GOMOD_VERSION)

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

sbom: $(SBOM_FILE) $(SBOM_EMBED)

$(SBOM_FILE): go.mod go.sum
	cyclonedx-gomod mod -json -licenses -output $@ .

$(SBOM_EMBED): $(SBOM_FILE)
	mkdir -p $(LICENSES_DIR)
	cp $(SBOM_FILE) $@

test:
	go test -race ./...

integration-test:
	go test -race -tags integration -count=1 -v ./pkg/integration/

feature-test:
	# Feature specs: drive the studio TUI through a real PTY + VT emulator.
	# Process-spawning, timing-sensitive, UNIX-only (creack/pty); out of `check`.
	go test -race -tags feature -count=1 -timeout 120s -v ./pkg/feature/

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

fuzz-seed:
	go test -run 'Fuzz' ./...

check: fmt vet lint test integration-test fuzz-seed

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
	rm -f $(LICENSES_FILE) $(PROJECT_LICENSE_EMBED) $(SBOM_FILE) $(SBOM_EMBED)
	find . -type f -name '*.test' -not -path './.git/*' -delete

linux: licenses sbom
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) $(RELEASE_TAGS) -o fizzle-linux-amd64 ./cmd/fizzle

darwin-arm64: licenses sbom
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) $(RELEASE_TAGS) -o fizzle-darwin-arm64 ./cmd/fizzle

darwin-amd64: licenses sbom
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) $(RELEASE_TAGS) -o fizzle-darwin-amd64 ./cmd/fizzle

windows: licenses sbom
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) $(RELEASE_TAGS) -o fizzle-windows-amd64.exe ./cmd/fizzle

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

.PHONY: build tools licenses sbom test integration-test feature-test fuzz-seed fmt vet lint check coverage benchmark profile install clean linux darwin-arm64 darwin-amd64 windows release demo asm-tools
