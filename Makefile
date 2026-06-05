.PHONY: build build-all build-darwin build-linux build-windows clean install release web-build web-dev dev production close kill-ports

APP      := ocode
VERSION  := $(shell grep "Version" internal/version/version.go | cut -d'"' -f2)
LDFLAGS  := -ldflags="-s -w"
OUTDIR   := release

# ── Default: build for current platform ──────────────────────────────────────

build: web-build
	go build $(LDFLAGS) -o $(APP) .

# ── Install ──────────────────────────────────────────────────────────────────

install: build
	go install .

# ── OS-specific builds (output to project root for convenience) ──────────────

build-darwin:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(APP)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(APP)-darwin-arm64 .

build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(APP)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(APP)-linux-arm64 .

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(APP)-windows-amd64.exe .

# ── Build all platforms ──────────────────────────────────────────────────────

build-all: build-darwin build-linux build-windows

# ── Release: versioned builds in a clean directory ──────────────────────────

release: clean
	@mkdir -p $(OUTDIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-$(VERSION)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-$(VERSION)-darwin-arm64 .
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-$(VERSION)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-$(VERSION)-linux-arm64 .
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(OUTDIR)/$(APP)-$(VERSION)-windows-amd64.exe .
	cd $(OUTDIR) && sha256sum $(APP)-* > sha256sums.txt
	@echo "\n✅ Release $(VERSION) built in $(OUTDIR)/"

# ── Clean ────────────────────────────────────────────────────────────────────

clean:
	rm -rf $(OUTDIR)
	rm -f $(APP) $(APP)-darwin-* $(APP)-linux-* $(APP)-windows-*

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
