.PHONY: build build-all build-darwin build-linux build-windows clean install release test web-build web-dev dev production close kill-ports models-snapshot docker-build docker docker-serve docker-run desktop install-desktop desktop-app desktop-icon-windows docker-desktop-darwin docker-desktop-linux docker-desktop-linux-arm docker-desktop-windows build-desktop-all docker-release

APP      := ocode
VERSION  := $(shell grep "Version" internal/version/version.go | cut -d'"' -f2)
LDFLAGS  := -ldflags="-s -w"
OUTDIR   := release

# ── Default: build for current platform ──────────────────────────────────────

build: web-build
	go build $(LDFLAGS) -o $(APP) .

# ── Install ──────────────────────────────────────────────────────────────────

install: web-build
	go build $(LDFLAGS) -o bin/$(APP) .
	go install $(LDFLAGS) .

# ── Desktop (Wails v3 shell) ────────────────────────────────────────────────
# Build the ocode-desktop binary. Requires cgo and native platform SDKs:
#   macOS: Xcode Command Line Tools
#   Linux: webkit2gtk-4.1-dev, libgtk-3-dev, etc.
#   Windows: WebView2 runtime
# See https://wails.io/docs/guides/platform-installation

DESKTOP_BUILD := go build $(LDFLAGS) -o bin/ocode-desktop ./cmd/ocode-desktop
DESKTOP_INSTALL := go install $(LDFLAGS) ./cmd/ocode-desktop

# macOS: Wails CGo objects may be compiled with a newer SDK; silence the
# version-mismatch linker warnings by matching the min version.
ifeq ($(shell uname), Darwin)
DESKTOP_BUILD := CGO_LDFLAGS="-mmacosx-version-min=26.0" $(DESKTOP_BUILD)
DESKTOP_INSTALL := CGO_LDFLAGS="-mmacosx-version-min=26.0" $(DESKTOP_INSTALL)
endif

# bundle-desktop-assets copies the bundled skills/plugins/model-configs next to
# the desktop main package so they can be embedded (go:embed cannot reach
# repo-root paths via ".."). The generated content is gitignored; a bare
# `go build ./cmd/ocode-desktop` still compiles against the committed .gitkeep.
bundle-desktop-assets:
	rm -rf cmd/ocode-desktop/embedded-assets
	mkdir -p cmd/ocode-desktop/embedded-assets/skills
	mkdir -p cmd/ocode-desktop/embedded-assets/.opencode/plugins
	cp -R skills/. cmd/ocode-desktop/embedded-assets/skills
	cp -R .opencode/plugins/. cmd/ocode-desktop/embedded-assets/.opencode/plugins
	# Copy every concrete (non-wildcard) model prompt. Files whose names
	# contain '*' (e.g. minimax-m*.OCODE.md, the disk-only wildcard catch-all)
	# are excluded: go:embed cannot embed such names, and they are not used by
	# the exact-name bundled fallback.
	find . -maxdepth 1 -name '*.OCODE.md' ! -name '*[*]*' -exec cp -f {} cmd/ocode-desktop/embedded-assets/ \;

desktop: web-build bundle-desktop-assets
	$(DESKTOP_BUILD)

install-desktop: web-build desktop desktop-app
	$(DESKTOP_INSTALL)

## desktop-app: build and bundle ocode.app (macOS only)
desktop-app: desktop
	./scripts/bundle-macos.sh bin/ocode-desktop bin/ocode.app

## desktop-icon-windows: regenerate the committed Windows .exe icon resource
## (cmd/ocode-desktop/rsrc_windows_*.syso) from cmd/ocode-desktop/winres/.
## go build auto-links these .syso files for GOOS=windows builds on any host;
## re-run this only when the icon or winres.json metadata changes.
## Requires: go install github.com/tc-hib/go-winres@latest
desktop-icon-windows:
	cd cmd/ocode-desktop && go-winres make --in winres/winres.json --out rsrc

# ── OS-specific builds (output to project root for convenience) ──────────────

build-darwin:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(APP)-darwin-amd64 . & \
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(APP)-darwin-arm64 . & \
	wait

build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(APP)-linux-amd64 . & \
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(APP)-linux-arm64 . & \
	wait

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(APP)-windows-amd64.exe .

# ── Build all platforms ──────────────────────────────────────────────────────

build-all:
	$(MAKE) build-darwin & \
	$(MAKE) build-linux & \
	$(MAKE) build-windows & \
	wait

# ── Release: versioned builds in a clean directory ──────────────────────────

release: clean
	@mkdir -p $(OUTDIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-$(VERSION)-darwin-amd64 . & \
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-$(VERSION)-darwin-arm64 . & \
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-$(VERSION)-linux-amd64 . & \
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-$(VERSION)-linux-arm64 . & \
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-$(VERSION)-windows-amd64.exe . & \
	wait
	cd $(OUTDIR) && sha256sum $(APP)-* > sha256sums.txt
	@echo "\n✅ Release $(VERSION) built in $(OUTDIR)/"

# ── Clean ────────────────────────────────────────────────────────────────────

clean:
	rm -rf $(OUTDIR)
	rm -f $(APP) $(APP)-darwin-* $(APP)-linux-* $(APP)-windows-*
	rm -rf bin/

# ── Test ─────────────────────────────────────────────────────────────────────
# Run all Go tests in the repo. Exits non-zero on any failure.
# Usage: make test

test:
	go test ./...

# ── Models snapshot ──────────────────────────────────────────────────────────
# Regenerate the embedded models.dev snapshot (gitignored build artifact) that
# backs context-window and pricing lookups. Run after models.dev publishes new
# models/prices, or after changing the retained field set in agent.modelEntry.
# Usage: make models-snapshot

models-snapshot:
	go run ./tools/gen-models-snapshot

# ── Web UI ───────────────────────────────────────────────────────────────────

web-build:
	cd web && pnpm install && pnpm run build

web-dev:
	cd web && pnpm run dev

# ── Kill processes on common ports ─────────────────────────────────────────────
# Usage: make kill-ports or make close

close: kill-ports

kill-ports:
	@echo "🔪 Killing processes on common ports..."
	@lsof -ti :4096 | xargs kill -9 2>/dev/null || true
	@lsof -ti :5173 | xargs kill -9 2>/dev/null || true
	@echo "✅ Done"

# ── Development ───────────────────────────────────────────────────────────────
# Run backend + frontend with hot reload. Requires: go, node/pnpm
# Usage: make dev

dev:
	@echo "🚀 Starting ocode development environment..."
	@echo "   Backend:  http://localhost:4096"
	@echo "   Frontend: http://localhost:5173"
	@echo "   Press Ctrl+C to stop"
	@echo ""
	@# Kill any existing processes on our ports
	@lsof -ti :4096 | xargs kill -9 2>/dev/null || true
	@lsof -ti :5173 | xargs kill -9 2>/dev/null || true
	@sleep 1
	@# Start backend in background
	@echo "📦 Starting Go backend..."
	@go run . serve --port 4096 &
	@sleep 2
	@# Start frontend in background
	@echo "🎨 Starting Vite frontend..."
	@cd web && pnpm run dev &
	@# Wait for both
	@wait

# ── Production build + serve ──────────────────────────────────────────────────
# Build web UI and run the Go server with embedded assets
# Usage: make production

production: web-build
	@echo "🏗️  Building Go binary with embedded web assets..."
	go build -o $(APP) .
	@echo "🚀 Starting production server on http://localhost:4096"
	@./$(APP) serve --port 4096

# ── Skill audit ─────────────────────────────────────────────────────────────
# Report skills that haven't been reviewed recently.
# Usage: make skill-audit

.PHONY: skill-audit
skill-audit:
	@echo "=== Skills last edited >14 days ago ==="
	@found=0; \
	for f in skills/*/SKILL.md; do \
	  d=$$(git log -1 --format=%cs -- "$$f" 2>/dev/null); \
	  if [ -z "$$d" ]; then d="never"; fi; \
	  if [ "$$d" = "never" ]; then \
	    echo "  ⚠️  $$f (never committed)"; \
	    found=1; \
	  else \
	    now=$$(date +%s); \
	    ts=$$(date -j -f "%Y-%m-%d" "$$d" +%s 2>/dev/null || date -d "$$d" +%s 2>/dev/null); \
	    if [ -n "$$ts" ]; then \
	      age=$$(( (now - ts) / 86400 )); \
	      if [ $$age -gt 14 ]; then \
	        echo "  ⚠️  $$f ($$age days old)"; \
	        found=1; \
	      fi; \
	    fi; \
	  fi; \
	done; \
	if [ $$found -eq 0 ]; then echo "  ✅ All skills are current."; fi
	@echo ""
	@echo "=== Changes since skill last edited ==="
	@for f in skills/*/SKILL.md; do \
	  d=$$(git log -1 --format=%cs -- "$$f" 2>/dev/null); \
	  if [ -n "$$d" ]; then \
	    skill=$$(basename $$(dirname "$$f")); \
	    case "$$skill" in \
	      ocode-agent-architecture) pkgs="internal/agent/";; \
	      ocode-tools) pkgs="internal/tool/ internal/agent/permissions.go";; \
	      ocode-tui) pkgs="internal/tui/";; \
	      ocode-permissions) pkgs="internal/agent/permissions.go internal/agent/agent_permissions.go internal/config/ocodeconfig.go";; \
	      custom-model-prompt) pkgs="internal/agent/context.go internal/agent/prompt.go";; \
	      ocode-usage) pkgs="internal/tui/ internal/config/ internal/agent/";; \
	      team-onboarding) pkgs="internal/ internal/tui/ internal/agent/ internal/tool/";; \
	      *) pkgs="";; \
	    esac; \
	    if [ -n "$$pkgs" ]; then \
	      changes=$$(git log --oneline --since="$$dT00:00:00" -- $$pkgs 2>/dev/null | wc -l); \
	      changes=$$(echo $$changes | tr -d ' '); \
	      if [ "$$changes" -gt 0 ] && [ "$$changes" != "0" ]; then \
	        echo "  📝 $$skill: $$changes change(s) in $$pkgs since $$d"; \
	      fi; \
	    fi; \
	  fi; \
	done

# ── Docker ─────────────────────────────────────────────────────────────────
# Build and run ocode inside Docker with volume mounts for config + data.
#
# Prerequisites: Docker Engine + Docker Compose v2
#
# Required host directories (created automatically on first run):
#   ~/.config/opencode/      — opencode.json + ocodeconfig.json
#   ~/.local/share/opencode/ — sessions, auth, usage records
#
# Usage:
#   make docker       # build + launch TUI interactively
#   make docker-serve # build + start web server on :4096
#   make docker-run   # run a headless command (set ARGS="..."):
#                     #   make docker-run ARGS="run --model claude-sonnet-4 'hello'"
#   make docker-build # build the image without running
# ─────────────────────────────────────────────────────────────────────────────

docker-build:
	@echo "🔨 Building ocode Docker image..."
	docker compose build

docker: docker-build
	@echo "🚀 Launching ocode TUI in Docker..."
	@echo "   Config:  ~/.config/opencode/ → container"
	@echo "   Data:    ~/.local/share/opencode/ → container"
	@echo "   Project: $(shell pwd) → /workspace"
	@echo ""
	docker compose run --rm ocode

docker-serve: docker-build
	@echo "🚀 Starting ocode web server on http://localhost:4096"
	docker compose up -d ocode-serve
	@echo "   Logs: docker compose logs -f ocode-serve"
	@echo "   Stop: docker compose down"

docker-run: docker-build
	@echo "🚀 Running: ocode $(ARGS)"
	docker compose run --rm ocode $(ARGS)

# ── Docker Cross-Compilation (macOS, Linux & Windows desktop) ─────────────
# Build ocode desktop binaries for all platforms. macOS is built locally
# (Docker can't cross-compile for macOS — needs native Apple frameworks).
# Linux and Windows are built via Docker.
#
# Usage:
#   make docker-desktop-darwin    # macOS amd64 + arm64 (local build)
#   make docker-desktop-linux     # Linux amd64 desktop binary
#   make docker-desktop-linux-arm # Linux arm64 desktop binary (requires QEMU)
#   make docker-desktop-windows   # Windows amd64 desktop binary
#   make docker-desktop-all       # All desktop targets (macOS + Linux + Windows)
#   make docker-release           # Alias for docker-desktop-all
# ─────────────────────────────────────────────────────────────────────────────

docker-desktop-darwin:
	@echo "🔨 Building macOS desktop binaries (local build)..."
	@mkdir -p release
	$(MAKE) web-build bundle-desktop-assets
	CGO_ENABLED=1 CGO_LDFLAGS="-mmacosx-version-min=26.0" GOOS=darwin GOARCH=amd64 \
	    go build $(LDFLAGS) -o release/ocode-desktop-darwin-amd64 ./cmd/ocode-desktop
	CGO_ENABLED=1 CGO_LDFLAGS="-mmacosx-version-min=26.0" GOOS=darwin GOARCH=arm64 \
	    go build $(LDFLAGS) -o release/ocode-desktop-darwin-arm64 ./cmd/ocode-desktop
	@test -s release/ocode-desktop-darwin-amd64 || (echo "❌ macOS amd64 build failed" && exit 1)
	@test -s release/ocode-desktop-darwin-arm64 || (echo "❌ macOS arm64 build failed" && exit 1)
	@chmod +x release/ocode-desktop-darwin-*
	@echo "✅ Built: release/ocode-desktop-darwin-amd64 ($$(du -h release/ocode-desktop-darwin-amd64 | cut -f1))"
	@echo "✅ Built: release/ocode-desktop-darwin-arm64 ($$(du -h release/ocode-desktop-darwin-arm64 | cut -f1))"

docker-desktop-linux:
	@echo "🔨 Building Linux amd64 desktop binary via Docker..."
	@mkdir -p release
	docker build -f Dockerfile.cross --target linux-desktop-out -o release/ .
	@test -s release/ocode-desktop-linux-amd64 || (echo "❌ Build failed: binary not found or empty" && exit 1)
	@chmod +x release/ocode-desktop-linux-amd64
	@echo "✅ Built: release/ocode-desktop-linux-amd64 ($$(du -h release/ocode-desktop-linux-amd64 | cut -f1))"

docker-desktop-linux-arm:
	@echo "🔨 Building Linux arm64 desktop binary via Docker (QEMU emulation)..."
	@mkdir -p release
	docker buildx build --platform linux/arm64 -f Dockerfile.cross --target linux-desktop-arm-out -o release/ .
	@test -s release/ocode-desktop-linux-arm64 || (echo "❌ Build failed: binary not found or empty" && exit 1)
	@chmod +x release/ocode-desktop-linux-arm64
	@echo "✅ Built: release/ocode-desktop-linux-arm64 ($$(du -h release/ocode-desktop-linux-arm64 | cut -f1))"

docker-desktop-windows:
	@echo "🔨 Building Windows amd64 desktop binary via Docker..."
	@mkdir -p release
	docker build -f Dockerfile.cross --target windows-desktop-out -o release/ .
	@test -s release/ocode-desktop-windows-amd64.exe || (echo "❌ Build failed: binary not found or empty" && exit 1)
	@echo "✅ Built: release/ocode-desktop-windows-amd64.exe ($$(du -h release/ocode-desktop-windows-amd64.exe | cut -f1))"

build-desktop-all:
	@echo "🔨 Building all desktop binaries..."
	@mkdir -p release
	@# Build macOS locally (Docker can't cross-compile for macOS)
	$(MAKE) docker-desktop-darwin
	@# Build Linux + Windows via Docker
	docker build -f Dockerfile.cross --target output -o release/ .
	@test -s release/ocode-desktop-darwin-amd64 || (echo "❌ macOS amd64 binary missing" && exit 1)
	@test -s release/ocode-desktop-darwin-arm64 || (echo "❌ macOS arm64 binary missing" && exit 1)
	@test -s release/ocode-desktop-linux-amd64 || (echo "❌ Linux amd64 binary missing" && exit 1)
	@test -s release/ocode-desktop-linux-arm64 || (echo "❌ Linux arm64 binary missing" && exit 1)
	@test -s release/ocode-desktop-windows-amd64.exe || (echo "❌ Windows binary missing" && exit 1)
	make desktop-app
	@chmod +x release/ocode-desktop-darwin-* release/ocode-desktop-linux-*
	@echo "✅ Built all desktop binaries:"
	@ls -lh release/ocode-desktop-*
	open release/

# Alias for backward compatibility with the plan
docker-release: build-desktop-all

install-all: install install-desktop

debug-desktop:
	bash scripts/capture-desktop-hang.sh
