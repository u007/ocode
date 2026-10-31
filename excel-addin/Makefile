# excel-addin: bridge (Go) + taskpane (TypeScript). Nothing here is part of the root ocode build.
.PHONY: test test-bridge test-pane build smoke manifest run-bridge install-excel

test: test-bridge test-pane

test-bridge:
	cd bridge && gofmt -l . | (! grep .) && go vet ./... && go test -race -count=1 ./...

test-pane:
	cd taskpane && pnpm install --frozen-lockfile --ignore-workspace && pnpm run typecheck && pnpm test

build:
	cd taskpane && pnpm install --frozen-lockfile --ignore-workspace && pnpm run build
	cd bridge && go build -o ../bin/excel-bridge .

# Real-browser run of the pane against the real bridge and a fake ocode (needs playwright + chromium).
smoke: build
	cd taskpane && node scripts/smoke-ui.mjs

# Needs EXCEL_BRIDGE_PANE_KEY (16+ chars). Prints where the manifest was written.
manifest:
	node scripts/gen-manifest.mjs

# macOS + Windows one-shot: dev cert, stable pane key, build, manifest, sideload. Re-run after `make test` (it clobbers the manifest).
install-excel:
	node scripts/install-excel.mjs

# Desktop run: HTTPS certs from `npx office-addin-dev-certs install`, ocode serve already running.
run-bridge: build
	./bin/excel-bridge -static taskpane/dist -cert $(CERT) -key $(KEY)
