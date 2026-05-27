.PHONY: build build-all build-darwin build-linux build-windows clean install release

APP      := ocode
VERSION  := $(shell grep "Version" internal/version/version.go | cut -d'"' -f2)
LDFLAGS  := -ldflags="-s -w"
OUTDIR   := release

# ── Default: build for current platform ──────────────────────────────────────

build:
	go build $(LDFLAGS) -o $(APP) .

# ── Install ──────────────────────────────────────────────────────────────────

install:
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
