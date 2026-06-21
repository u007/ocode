.PHONY: build build-all build-darwin build-linux build-windows clean install release test web-build web-dev dev production close kill-ports models-snapshot docker-build docker docker-serve docker-run

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
	cd web && npm install && npm run build

web-dev:
	cd web && npm run dev

# ── Kill processes on common ports ─────────────────────────────────────────────
# Usage: make kill-ports or make close

close: kill-ports

kill-ports:
	@echo "🔪 Killing processes on common ports..."
	@lsof -ti :4096 | xargs kill -9 2>/dev/null || true
	@lsof -ti :5173 | xargs kill -9 2>/dev/null || true
	@echo "✅ Done"

# ── Development ───────────────────────────────────────────────────────────────
# Run backend + frontend with hot reload. Requires: go, node/npm
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
	@cd web && npm run dev &
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
