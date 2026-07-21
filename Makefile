# Root build orchestrator for aigenaudiotools.
#
# Two modules live here (see DESIGN.md): the engine + CLI (this module, zero
# third-party deps) and the desktop app (wails/, its own module). This Makefile
# drives the engine/CLI directly and delegates app builds to wails/Makefile.
#
#   make verify        build + vet + test the engine and CLI (host)
#   make check-cross   compile-check every package for all 5 target platforms
#   make cli           build the CLI for this host -> dist/
#   make cli-all       cross-compile the CLI for all 5 platforms -> dist/
#   make app           build the desktop app FOR THIS HOST ONLY (see note below)
#   make dist          cli-all + the host app
#   make clean
#
# Wails cannot cross-compile: it needs cgo and each OS's native webview
# (WebKit / WebView2 / WebKitGTK). So `make app` only ever produces the app for
# the machine you run it on. To ship the app for Windows and Linux too, build on
# those OSes or in CI with per-OS runners (`make ci-hint`). The CLI has no such
# limit — `make cli-all` builds all 5 from any host.

# This repo is standalone but sits inside a parent go.work that doesn't list it,
# so every go command must ignore that workspace.
export GOWORK := off

BIN     := aigenaudio
DIST    := dist
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w

# Requested targets. "x86" here means 64-bit Intel/AMD (amd64), the standard
# desktop target; add darwin/amd64 or windows/386 to this list if ever needed.
CLI_PLATFORMS := darwin/arm64 windows/amd64 windows/arm64 linux/amd64 linux/arm64

.DEFAULT_GOAL := help

.PHONY: help verify check-cross cli cli-all app dist clean ci-hint

help:
	@echo "aigenaudiotools — build targets:"
	@echo "  make verify        build + vet + test the engine and CLI"
	@echo "  make check-cross   compile-check all packages for every target OS/arch"
	@echo "  make cli           build the CLI for this host -> $(DIST)/"
	@echo "  make cli-all       cross-compile the CLI for all 5 platforms -> $(DIST)/"
	@echo "  make app           build the desktop app for THIS host only"
	@echo "  make dist          cli-all + the host app"
	@echo "  make clean"
	@echo ""
	@echo "CLI targets: $(CLI_PLATFORMS)"
	@echo "Note: the Wails app can't cross-compile; 'make app' is host-only. See 'make ci-hint'."

# --- engine + CLI (this module) ---------------------------------------------

verify:
	go build ./...
	go vet ./...
	go test ./...

# Cheap portability net: compile every package (library + CLI) for each target
# without producing binaries. Catches OS-specific mistakes early.
check-cross:
	@for p in $(CLI_PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		printf "  checking %-14s" "$$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build ./... && echo "ok" || exit 1; \
	done

cli:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BIN) ./cmd/cli
	@echo "built $(DIST)/$(BIN)"

# Pure Go, so this genuinely produces all 5 binaries from any host.
cli-all:
	@mkdir -p $(DIST)
	@for p in $(CLI_PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		out=$(DIST)/$(BIN)_$${os}_$${arch}; \
		[ "$$os" = "windows" ] && out=$$out.exe; \
		printf "  building %-14s -> %s\n" "$$os/$$arch" "$$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags '$(LDFLAGS)' -o $$out ./cmd/cli || exit 1; \
	done
	@echo "CLI binaries in $(DIST)/ (version $(VERSION))"

# --- desktop app (wails/ module) --------------------------------------------

# Host-only by nature — see the note at the top of this file. Delegates to
# wails/Makefile, which also compiles Tailwind before building.
app:
	$(MAKE) -C wails build

# --- aggregate / housekeeping -----------------------------------------------

dist: cli-all app

clean:
	rm -rf $(DIST)
	$(MAKE) -C wails clean

ci-hint:
	@echo "Wails builds run on native OS runners. In GitHub Actions use a matrix:"
	@echo "  strategy.matrix.os: [macos-latest, windows-latest, ubuntu-latest]"
	@echo "  steps: install the wails CLI, then 'cd wails && make build'."
	@echo "The CLI needs no matrix — one 'make cli-all' on any runner emits all 5."
	@echo "Ask and I can scaffold .github/workflows/release.yml."
