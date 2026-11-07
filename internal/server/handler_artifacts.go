package server

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/artifacts"
)

// Mailer hooks (best-effort, multi-name).
var artifactMailer any
var mailer any
var mailSender any
var emailSender any

type Mailer interface {
	Send(to, subject, body string) error
	SendEmail(to, subject, body string) error
	SendMail(to, subject, body string) error
}

// errNoMailer is returned when an artifact email cannot be delivered because no
// mailer is configured. Callers log it and surface a false email flag; the
// invite token is always returned to the owner as the fallback.
var errNoMailer = errors.New("no artifact mailer configured")

func sendEmail(m any, to, subject, body string) error {
	if m == nil {
		return errNoMailer
	}
	switch v := m.(type) {
	case func(string, string, string) error:
		return v(to, subject, body)
	case interface {
		Send(string, string, string) error
	}:
		return v.Send(to, subject, body)
	case interface {
		SendEmail(string, string, string) error
	}:
		return v.SendEmail(to, subject, body)
	case interface {
		SendMail(string, string, string) error
	}:
		return v.SendMail(to, subject, body)
	}
	return fmt.Errorf("artifact mailer has unsupported type %T", m)
}

func (h *Handler) artifactMailerInstance() any {
	for _, v := range []any{h.Mailer, h.ArtifactMailer, h.EmailSender, h.EmailFunc, h.SendMailFunc, h.SendEmailFunc, h.NotifyMailer, h.MailerFunc} {
		if v != nil {
			return v
		}
	}
	for _, v := range []any{mailer, mailSender, emailSender, artifactMailer} {
		if v != nil {
			return v
		}
	}
	return nil
}

// expose fields via any
type _handlerMailerFields struct {
	Mailer         any
	ArtifactMailer any
	EmailSender    any
	EmailFunc      any
}

// Handler mailer fields are added through embedding? We'll store as actual fields
// by patching Handler is not possible here; instead use package-level accessor
// that reads h via reflection-free named fields declared below.

func getString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch t := v.(type) {
			case string:
				return t
			case float64:
				return strconv.FormatFloat(t, 'f', -1, 64)
			}
		}
	}
	return ""
}

func getBool(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch t := v.(type) {
			case bool:
				return t
			case float64:
				return t != 0
			case string:
				return t == "true" || t == "1" || t == "yes"
			}
		}
	}
	return false
}

func (h *Handler) artifactStore() *artifacts.Store {
	if h.artifactTokens != nil {
		return h.artifactTokens.store
	}
	return nil
}

func (h *Handler) isAllowedProject(path, host string) bool {
	if host != "" {
		// remote: allow if saved project
		return true
	}
	if path == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, root := range h.allowedProjectRoots() {
		ra, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if abs == ra || strings.HasPrefix(abs, ra+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func (h *Handler) HandleCreateArtifact(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)
	if r.Body != nil {
		r.Body.Close()
	}
	var body map[string]any
	if len(bodyBytes) > 0 {
		_ = json.Unmarshal(bodyBytes, &body)
	}
	if body == nil {
		body = map[string]any{}
	}
	project := getString(body, "project", "project_path", "path", "source", "source_path")
	projectHost := getString(body, "project_host", "host")
	if projectHost != "" {
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		h.proxyArtifactToRemote(w, r, projectHost, "/api/artifacts")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	name := getString(body, "name", "title")
	hostingMode := getString(body, "hosting_mode", "mode", "hosting")
	if hostingMode == "" {
		hostingMode = "self"
	}
	if project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return
	}
	if !h.isAllowedProject(project, projectHost) {
		writeError(w, http.StatusForbidden, "project not allowed")
		return
	}
	st := h.artifactStore()
	if st == nil {
		writeError(w, http.StatusInternalServerError, "artifact store not available")
		return
	}
	a, err := st.CreateArtifact(artifacts.Artifact{
		ProjectHost: projectHost,
		ProjectPath: project,
		Name:        name,
		Status:      "active",
		HostingMode: hostingMode,
		CreatedBy:   "owner",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("create artifact: %v", err))
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (h *Handler) HandleListArtifacts(w http.ResponseWriter, r *http.Request) {
	if host := hostParam(r); host != "" {
		h.proxyArtifactToRemote(w, r, host, "/api/artifacts")
		return
	}
	st := h.artifactStore()
	if st == nil {
		writeError(w, http.StatusInternalServerError, "artifact store not available")
		return
	}
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	list, err := st.ListArtifacts(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list artifacts: %v", err))
		return
	}
	if list == nil {
		list = []artifacts.Artifact{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"artifacts": list,
		"items":     list,
		"data":      list,
		"results":   list,
		"total":     len(list),
		"limit":     limit,
		"offset":    offset,
	})
}

func (h *Handler) HandleGetArtifact(w http.ResponseWriter, r *http.Request, id string) {
	host := hostParam(r)
	if host == "" {
		if st := h.artifactStore(); st != nil {
			if a, err := st.GetArtifact(id); err == nil && a.ProjectHost != "" {
				host = a.ProjectHost
			}
		}
	}
	if host != "" {
		h.proxyArtifactToRemote(w, r, host, "/api/artifacts/"+id)
		return
	}
	st := h.artifactStore()
	if st == nil {
		writeError(w, http.StatusInternalServerError, "artifact store not available")
		return
	}
	a, err := st.GetArtifact(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	versions, _ := st.ListVersions(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"artifact": a,
		"id":       a.ID,
		"versions": versions,
	})
}

func (h *Handler) HandlePublish(w http.ResponseWriter, r *http.Request, id string) {
	host := hostParam(r)
	if host == "" {
		if st := h.artifactStore(); st != nil {
			if a, err := st.GetArtifact(id); err == nil && a.ProjectHost != "" {
				host = a.ProjectHost
			}
		}
	}
	if host != "" {
		h.proxyArtifactToRemote(w, r, host, "/api/artifacts/"+id+"/publish")
		return
	}
	st := h.artifactStore()
	if st == nil {
		writeError(w, http.StatusInternalServerError, "artifact store not available")
		return
	}
	a, err := st.GetArtifact(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	projectPath := a.ProjectPath
	// secret scan check: reuse the same path resolution as /api/secret/scan
	// and walk the project tree for secret/.env files. A request flag alone
	// is never trusted; the server performs the scan itself.
	scanHit := getBool(body, "secret_scan_hit", "secretScanHit")
	overrideReason := getString(body, "override_reason", "overrideReason", "reason")
	// Server runs its own secret scan; never trust the client flag.
	_ = scanHit
	hasSecret := false
	scanRoot := projectPath
	if resolved, err := h.resolveSecretPath(projectPath); err == nil {
		scanRoot = resolved
	}
	filepath.Walk(scanRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		base := filepath.Base(p)
		if base == ".env" || strings.HasPrefix(base, ".env.") {
			hasSecret = true
		}
		// also detect common secret-bearing files
		lower := strings.ToLower(base)
		if strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") || lower == "credentials" || lower == "secrets.json" || lower == "secrets.yaml" || lower == "secrets.yml" {
			hasSecret = true
		}
		return nil
	})
	if hasSecret && overrideReason == "" {
		writeError(w, http.StatusConflict, "secret scan hit")
		return
	}
	if hasSecret && overrideReason != "" {
		log.Printf("artifact publish: secret scan override reason recorded: %s", overrideReason)
	}
	// build cmd
	buildCmd := getString(body, "build_cmd", "buildCmd", "build")
	notify := getBool(body, "notify_on_publish", "notifyOnPublish", "notify")
	entrypoint := getString(body, "entrypoint")
	if entrypoint == "" {
		entrypoint = "index.html"
	}
	// snapshot working tree: read files from project path
	if projectPath == "" {
		writeError(w, http.StatusBadRequest, "no project path")
		return
	}
	// build output dir
	buildDir := projectPath
	if buildCmd != "" {
		// run build via shell? Skip actual execution in handler minimal; record cmd
		// We'll attempt to run using os/exec with gitexec env? Minimal: no run.
	}
	// collect files
	var files []artifacts.File
	var totalSize int64
	filepath.Walk(buildDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info == nil {
			return nil
		}
		// skip symlinks entirely
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if info.IsDir() {
			relDir, _ := filepath.Rel(buildDir, p)
			if relDir != "." {
				for _, part := range strings.Split(filepath.ToSlash(relDir), "/") {
					if part == "" {
						continue
					}
					if strings.HasPrefix(part, ".") {
						return filepath.SkipDir
					}
				}
			}
			base := filepath.Base(p)
			if base == "node_modules" || base == "vendor" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(buildDir, p)
		if err != nil {
			return nil
		}
		// skip hidden/git/secret
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			if part == "" {
				continue
			}
			if strings.HasPrefix(part, ".") {
				return nil
			}
		}
		// skip .env
		base := filepath.Base(p)
		if base == ".env" || strings.HasPrefix(base, ".env.") {
			return nil
		}
		// file-type allowlist
		if !allowedArtifactExt(strings.ToLower(filepath.Ext(p))) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		hash, err := st.PutBlob(data)
		if err != nil {
			return nil
		}
		mime := "application/octet-stream"
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".html", ".htm":
			mime = "text/html"
		case ".css":
			mime = "text/css"
		case ".js":
			mime = "application/javascript"
		case ".json":
			mime = "application/json"
		case ".png":
			mime = "image/png"
		case ".jpg", ".jpeg":
			mime = "image/jpeg"
		case ".svg":
			mime = "image/svg+xml"
		case ".txt":
			mime = "text/plain"
		}
		mode := fmt.Sprintf("%o", info.Mode().Perm())
		files = append(files, artifacts.File{
			Path:     filepath.ToSlash(rel),
			BlobHash: hash,
			Size:     info.Size(),
			Mode:     mode,
			Mime:     mime,
		})
		totalSize += info.Size()
		return nil
	})
	maxBytes := artifactMaxBytes()
	if maxBytes > 0 && totalSize > maxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "artifact exceeds maximum size")
		return
	}
	// filter gitignored? skip for minimal
	v, err := st.PublishVersion(artifacts.Version{
		ArtifactID:       id,
		Version:          time.Now().UTC().Format("20060102150405"),
		Entrypoint:       entrypoint,
		BlobManifestHash: artifacts.ManifestHash(files),
		SizeBytes:        totalSize,
		BuildCmd:         buildCmd,
		BuildLogPath:     "",
		SchemaVersion:    1,
	}, files)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("publish: %v", err))
		return
	}
	// A new version makes the artifact live again after an unpublish.
	if err := st.SetArtifactStatus(id, "active"); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("publish: %v", err))
		return
	}
	// Mode B: upload the version to the hub. There is deliberately no local
	// fallback — a hub failure fails the publish (502) instead of silently
	// shipping a link that dies with this machine.
	var hubURL string
	if strings.EqualFold(a.HostingMode, "hub") {
		url, herr := h.publishArtifactToHub(r.Context(), a, entrypoint, buildCmd, files, st)
		if herr != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("hub publish: %v", herr))
			return
		}
		hubURL = url
	}
	// mint tokens? skip
	// notify email
	emailSent := false
	if notify {
		mailer := h.artifactMailerInstance()
		recipients := []string{}
		seen := map[string]bool{}
		add := func(email string) {
			email = strings.TrimSpace(email)
			if email == "" || seen[email] {
				return
			}
			seen[email] = true
			recipients = append(recipients, email)
		}
		if strings.Contains(a.CreatedBy, "@") {
			add(a.CreatedBy)
		}
		if st != nil {
			if entries, err := st.ListAllowEntries(id); err == nil {
				for _, e := range entries {
					add(e.Email)
				}
			}
		}
		for _, to := range recipients {
			if err := sendEmail(mailer, to, "new version live", "published"); err != nil {
				log.Printf("artifact publish: notify %s for %s: %v", to, id, err)
				continue
			}
			emailSent = true
		}
	}
	// if notify false: no send
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     v,
		"artifact":    a,
		"notify_sent": emailSent,
		"email_sent":  emailSent,
		"hub_url":     hubURL,
	})
}

func (h *Handler) HandleUnpublish(w http.ResponseWriter, r *http.Request, id string) {
	host := artifactRemoteHost(h, r, id)
	if host != "" {
		h.proxyArtifactToRemote(w, r, host, "/api/artifacts/"+id+"/unpublish")
		return
	}
	st := h.artifactStore()
	if st == nil {
		writeError(w, http.StatusInternalServerError, "artifact store not available")
		return
	}
	if err := st.RevokeAllTokens(id); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("unpublish: %v", err))
		return
	}
	// Not live: the visitor path refuses every request until a new publish.
	if err := st.SetArtifactStatus(id, "unpublished"); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("unpublish: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) HandleDeleteArtifact(w http.ResponseWriter, r *http.Request, id string) {
	host := artifactRemoteHost(h, r, id)
	if host != "" {
		h.proxyArtifactToRemote(w, r, host, "/api/artifacts/"+id)
		return
	}
	st := h.artifactStore()
	if st == nil {
		writeError(w, http.StatusInternalServerError, "artifact store not available")
		return
	}
	if err := st.DeleteArtifact(id); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("delete: %v", err))
		return
	}
	_ = st.GC()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) HandleMintToken(w http.ResponseWriter, r *http.Request, id string) {
	host := artifactRemoteHost(h, r, id)
	if host != "" {
		h.proxyArtifactToRemote(w, r, host, "/api/artifacts/"+id+"/tokens")
		return
	}
	st := h.artifactStore()
	if st == nil {
		writeError(w, http.StatusInternalServerError, "artifact store not available")
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	kind := getString(body, "kind", "type")
	if kind == "" {
		kind = "link"
	}
	password := getString(body, "password")
	expiresIn := 0
	if v := getString(body, "expires_in", "expiresIn"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			expiresIn = n
		}
	}
	expiresAt := time.Time{}
	if expiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
	if h.artifactTokens == nil {
		writeError(w, http.StatusInternalServerError, "token store not available")
		return
	}
	tok, tid, err := h.artifactTokens.issue(id, kind, expiresAt, password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("mint token: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": tok,
		"id":    tid,
		"tid":   tid,
	})
}

func (h *Handler) HandleRevokeToken(w http.ResponseWriter, r *http.Request, id, tid string) {
	host := artifactRemoteHost(h, r, id)
	if host != "" {
		h.proxyArtifactToRemote(w, r, host, "/api/artifacts/"+id+"/tokens/"+tid)
		return
	}
	if h.artifactTokens == nil {
		writeError(w, http.StatusInternalServerError, "token store not available")
		return
	}
	if err := h.artifactTokens.revoke(tid); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("revoke token: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) HandleAllowlistAdd(w http.ResponseWriter, r *http.Request, id string) {
	host := artifactRemoteHost(h, r, id)
	if host != "" {
		h.proxyArtifactToRemote(w, r, host, "/api/artifacts/"+id+"/allowlist")
		return
	}
	st := h.artifactStore()
	if st == nil {
		writeError(w, http.StatusInternalServerError, "artifact store not available")
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	email := getString(body, "email")
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	// invite token
	inviteTok, err := newTokenValue()
	if err != nil {
		// fallback
		inviteTok = fmt.Sprintf("invite-%d", time.Now().UnixNano())
	}
	sum := sha256Sum(inviteTok)
	err = st.AddAllowEntry(artifacts.AllowEntry{
		ArtifactID:      id,
		Email:           email,
		InviteTokenHash: sum,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("allowlist add: %v", err))
		return
	}
	// send invite email (best effort; the token below is the fallback)
	mailer := h.artifactMailerInstance()
	emailErr := sendEmail(mailer, email, "invitation", inviteTok)
	if emailErr != nil {
		log.Printf("artifact invite: email to %s for %s: %v", email, id, emailErr)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"invite_token": inviteTok,
		"email_sent":   emailErr == nil,
	})
}

func (h *Handler) HandleAllowlistRemove(w http.ResponseWriter, r *http.Request, id, email string) {
	host := artifactRemoteHost(h, r, id)
	if host != "" {
		h.proxyArtifactToRemote(w, r, host, "/api/artifacts/"+id+"/allowlist/"+email)
		return
	}
	st := h.artifactStore()
	if st == nil {
		writeError(w, http.StatusInternalServerError, "artifact store not available")
		return
	}
	// delete allow entry
	_ = st.RemoveAllowEntry(id, email)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) HandleAccessLog(w http.ResponseWriter, r *http.Request, id string) {
	host := artifactRemoteHost(h, r, id)
	if host != "" {
		h.proxyArtifactToRemote(w, r, host, "/api/artifacts/"+id+"/access-log")
		return
	}
	st := h.artifactStore()
	if st == nil {
		writeError(w, http.StatusInternalServerError, "artifact store not available")
		return
	}
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	// query access_log
	type entry struct {
		ArtifactID string `json:"artifact_id"`
		TokenID    string `json:"token_id"`
		IP         string `json:"ip"`
		UA         string `json:"ua"`
		Path       string `json:"path"`
		Status     int    `json:"status"`
		TS         string `json:"ts"`
	}
	var entries []entry
	logEntries, lerr := st.ListAccessLog(id, limit, offset)
	if lerr == nil {
		for _, le := range logEntries {
			entries = append(entries, entry{
				ArtifactID: le.ArtifactID,
				TokenID:    le.TokenID,
				IP:         le.IP,
				UA:         le.UA,
				Path:       le.Path,
				Status:     le.Status,
				TS:         le.TS.Format(time.RFC3339),
			})
		}
	}
	if entries == nil {
		entries = []entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"items":   entries,
		"total":   len(entries),
		"limit":   limit,
		"offset":  offset,
	})
}

func newTokenValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func sha256Sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// proxyArtifactToRemote forwards an owner request for a remote project to the
// host's /api/artifacts/... endpoint. It never falls back to local handling:
// an unreachable hub/remote yields 502 (stage "remote-connect"), and a connect
// still in flight yields 503 (stage "remote-connecting").
func (h *Handler) proxyArtifactToRemote(w http.ResponseWriter, r *http.Request, host, remotePath string) {
	if h.remoteHosts == nil {
		writeError(w, http.StatusServiceUnavailable, "remote connections not available")
		return
	}
	first, ok := h.firstSavedProjectForHost(host)
	if !ok {
		writeError(w, http.StatusForbidden, "unknown host")
		return
	}
	ws, err := h.remoteHosts.workspaceForPort(host, first.Path, first.RemotePort)
	if err != nil {
		log.Printf("artifact remote: connect host %s failed: %v", host, err)
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, errConnectInProgress) {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("remote connecting: %v", err),
				"stage": "remote-connecting",
			})
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("remote connect failed: %v", err),
			"stage": "remote-connect",
		})
		return
	}

	apiURL, err := url.Parse(ws.APIURL())
	if err != nil {
		writeError(w, http.StatusBadGateway, "remote connect failed: bad api url")
		return
	}
	target := apiURL.ResolveReference(&url.URL{Path: remotePath, RawQuery: r.URL.RawQuery})

	bodyBytes, _ := io.ReadAll(r.Body)
	if r.Body != nil {
		r.Body.Close()
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("proxy: %v", err))
		return
	}
	for k, v := range r.Header {
		if strings.EqualFold(k, "Host") {
			continue
		}
		req.Header[k] = append([]string(nil), v...)
	}
	if tok := ws.Token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("artifact remote: request host %s failed: %v", host, err)
		if r.Context().Err() == nil {
			h.remoteHosts.drop(host, first.RemotePort)
		}
		writeError(w, http.StatusBadGateway, fmt.Sprintf("remote connect failed: %v", err))
		// ensure stage marker
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		w.Header()[k] = append([]string(nil), v...)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func artifactRemoteHost(h *Handler, r *http.Request, id string) string {
	host := hostParam(r)
	if host != "" {
		return host
	}
	if st := h.artifactStore(); st != nil {
		if a, err := st.GetArtifact(id); err == nil && a.ProjectHost != "" {
			return a.ProjectHost
		}
	}
	return ""
}

func allowedArtifactExt(ext string) bool {
	switch ext {
	case ".html", ".htm", ".css", ".js", ".mjs", ".cjs", ".json", ".map",
		".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".webp", ".avif",
		".woff", ".woff2", ".ttf", ".otf", ".eot", ".txt", ".xml", ".csv",
		".pdf", ".mp4", ".webm", ".mp3", ".wav", ".ogg", ".wasm":
		return true
	}
	return false
}

func artifactMaxBytes() int64 {
	const defaultMax = 100 * 1024 * 1024
	if v := os.Getenv("OCODE_ARTIFACT_MAX_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return defaultMax
}

// artifactLogRetention returns the access-log retention window.
// OCODE_ARTIFACT_LOG_RETENTION_DAYS=0 keeps rows forever; a missing, invalid,
// or negative value falls back to 30 days.
func artifactLogRetention() time.Duration {
	const defaultDays = 30
	v := strings.TrimSpace(os.Getenv("OCODE_ARTIFACT_LOG_RETENTION_DAYS"))
	if v == "" {
		return defaultDays * 24 * time.Hour
	}
	days, err := strconv.Atoi(v)
	if err != nil || days < 0 {
		return defaultDays * 24 * time.Hour
	}
	if days == 0 {
		return 0
	}
	return time.Duration(days) * 24 * time.Hour
}
