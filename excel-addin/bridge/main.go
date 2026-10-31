// Command bridge connects the Excel task pane to ocode serve.
//
//	pane (HTTPS, same origin) --/api/*--> bridge --Bearer--> ocode serve
//	ocode --POST /mcp (tools/call)--> bridge --SSE call / POST result--> pane (Office.js)
//
// See excel-addin/README.md and excel-addin/RESUME.md.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	wsDefault, wsErr := defaultWorkspaceRoot()
	stateDefault, stateErr := defaultStateDir()
	var (
		listen   = flag.String("listen", env("EXCEL_BRIDGE_LISTEN", "127.0.0.1:3443"), "listen address (loopback)")
		ocode    = flag.String("ocode", env("EXCEL_BRIDGE_OCODE_URL", "http://127.0.0.1:4096"), "ocode serve base URL")
		static   = flag.String("static", env("EXCEL_BRIDGE_STATIC", ""), "directory with the built task pane (taskpane/dist)")
		certFile = flag.String("cert", env("EXCEL_BRIDGE_CERT", ""), "TLS certificate (PEM); Office needs HTTPS")
		keyFile  = flag.String("key", env("EXCEL_BRIDGE_KEY", ""), "TLS private key (PEM)")
		origins  = flag.String("allow-origin", env("EXCEL_BRIDGE_ALLOW_ORIGINS", ""), "extra allowed browser origins, comma separated")
		timeout  = flag.Duration("call-timeout", 90*time.Second, "max time for one Excel tool call")
		wsRoot   = flag.String("workspace-root", env("EXCEL_BRIDGE_WORKSPACE", wsDefault), "one folder per workbook, each registered with ocode as a project; <data dir> is ocode's own data dir")
		dataDir  = flag.String("data-dir", env("EXCEL_BRIDGE_DATA", stateDefault), "pane state kept across restarts (undo journal, session index); empty value means the default")
	)
	flag.Parse()
	// An empty value means "use the default". It never means "keep state in memory": a flag must not silently
	// turn persistence off. The data dir is <data dir>/excel, where <data dir> is the one ocode uses (datadir.go).
	if *wsRoot == "" {
		*wsRoot = wsDefault
	}
	if *dataDir == "" {
		*dataDir = stateDefault
	}
	if *wsRoot == "" || *dataDir == "" {
		log.Fatalf("no data directory: %v (pass --workspace-root and --data-dir)", errors.Join(wsErr, stateErr))
	}

	// The ocode token comes from the environment only (never a flag: flags show up in process lists).
	token := os.Getenv("OCODE_TOKEN")
	key := os.Getenv("EXCEL_BRIDGE_PANE_KEY")
	if key == "" {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			log.Fatalf("generating pane key: %v", err)
		}
		key = hex.EncodeToString(b)
	}
	cfg := Config{OcodeURL: *ocode, OcodeToken: token, PaneKey: key, StaticDir: *static, CallTimeout: *timeout, DataDir: *dataDir, WorkspaceRoot: *wsRoot}
	if *origins != "" {
		cfg.AllowedOrigins = strings.Split(*origins, ",")
	}
	h, _, err := NewHandler(cfg)
	if err != nil {
		log.Fatal(err)
	}
	scheme := "http"
	if *certFile != "" {
		scheme = "https"
	}
	fmt.Printf("excel bridge on %s://%s\n  task pane URL:   %s://%s/taskpane.html?key=%s\n  ocode MCP entry: {\"type\":\"remote\",\"url\":\"%s://%s/mcp\",\"headers\":{\"X-Bridge-Key\":\"%s\"},\"timeout\":120000}\n",
		scheme, *listen, scheme, *listen, key, scheme, *listen, key)
	srv := &http.Server{Addr: *listen, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	if *certFile != "" {
		log.Fatal(srv.ListenAndServeTLS(*certFile, *keyFile))
	}
	log.Fatal(srv.ListenAndServe())
}
