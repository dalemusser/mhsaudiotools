# Desktop app tasks. The engine is a separate module (see go.mod) — build it
# from the repo root (`make -C .. cli-all`, etc.).
#
#   make setup   download the Tailwind standalone CLI (once, per machine)
#   make css     compile frontend/tailwind.css -> frontend/src/app.css
#   make watch   recompile CSS on change
#   make dev     run the app with hot reload
#   make build   produce an app bundle for THIS host in build/bin
#
# Wails cannot cross-compile (cgo + native webviews), so `build` targets the host
# OS only. It builds for the host's own architecture by default; set ARCH to
# build the other arch of the SAME OS if your toolchain supports it, e.g.
# `make build ARCH=arm64` on an Intel Mac, or ARCH=universal on macOS.

# Ignore the parent go.work that doesn't list this module (matches the root Makefile).
export GOWORK := off

# Host OS's other-architecture build, when supported (mainly macOS universal).
ARCH ?=
WAILS_PLATFORM := $(if $(ARCH),-platform $(shell go env GOOS)/$(ARCH),)

TAILWIND     := frontend/.tools/tailwindcss
TAILWIND_IN  := frontend/tailwind.css
TAILWIND_OUT := frontend/src/app.css

# Platform-specific standalone binary, matching the pattern used elsewhere in
# the org (no npm / node_modules needed).
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)
ifeq ($(UNAME_S),Darwin)
  ifeq ($(UNAME_M),arm64)
    TAILWIND_ASSET := tailwindcss-macos-arm64
  else
    TAILWIND_ASSET := tailwindcss-macos-x64
  endif
else
  ifeq ($(UNAME_M),aarch64)
    TAILWIND_ASSET := tailwindcss-linux-arm64
  else
    TAILWIND_ASSET := tailwindcss-linux-x64
  endif
endif

TAILWIND_URL := https://github.com/tailwindlabs/tailwindcss/releases/latest/download/$(TAILWIND_ASSET)

.PHONY: setup css watch dev build clean

setup: $(TAILWIND)

$(TAILWIND):
	@mkdir -p $(dir $(TAILWIND))
	@echo "downloading $(TAILWIND_ASSET)…"
	@curl -sSL -o $(TAILWIND) $(TAILWIND_URL)
	@chmod +x $(TAILWIND)
	@$(TAILWIND) --version

css: $(TAILWIND)
	@$(TAILWIND) -i $(TAILWIND_IN) -o $(TAILWIND_OUT) --minify
	@echo "wrote $(TAILWIND_OUT)"

watch: $(TAILWIND)
	@$(TAILWIND) -i $(TAILWIND_IN) -o $(TAILWIND_OUT) --watch

dev: css
	wails dev

build: css
	wails build $(WAILS_PLATFORM)

clean:
	rm -rf build/bin
