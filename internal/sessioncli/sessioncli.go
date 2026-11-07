package sessioncli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultPort is the port `ocode serve` listens on by default.
const defaultPort = 4096

const usageText = `usage:
  ocode session status [flags] <session_id>
  ocode session send   [flags] --yes <session_id> <content...>
  ocode session queue  [flags] --yes <session_id> <content...>

flags:
  --url <url>      server base URL (default http://localhost:<port>)
  --port <n>       server port on localhost (default 4096)
  -u <name>        basic auth username (default OPENCODE_SERVER_USERNAME, or "opencode" when a password is set)
  -P <password>    basic auth password (default OPENCODE_SERVER_PASSWORD)
  --yes            required for send and queue: they write into another session`

var httpClient = &http.Client{Timeout: 60 * time.Second}

// Run is the entry point for `ocode session ...`.
func Run(args []string) error {
	if len(args) == 0 {
		return errors.New(usageText)
	}
	switch args[0] {
	case "status":
		return statusCmd(args[1:])
	case "send":
		return postCmd("send", args[1:], false)
	case "queue":
		return postCmd("queue", args[1:], true)
	default:
		return fmt.Errorf("unknown subcommand %q\n%s", args[0], usageText)
	}
}

// connFlags are the flags every subcommand accepts to reach the server.
type connFlags struct {
	url      string
	port     int
	username string
	password string
	yes      bool
}

// parseConnFlags reads the subcommand's flags. yes is only defined for the
// write subcommands. It returns the positional arguments that follow the flags.
func parseConnFlags(name string, args []string, withYes bool) (connFlags, []string, error) {
	var f connFlags
	fs := flag.NewFlagSet("ocode session "+name, flag.ContinueOnError)
	fs.StringVar(&f.url, "url", "", "server base URL")
	fs.IntVar(&f.port, "port", 0, "server port on localhost")
	fs.StringVar(&f.username, "u", "", "basic auth username")
	fs.StringVar(&f.password, "P", "", "basic auth password")
	if withYes {
		fs.BoolVar(&f.yes, "yes", false, "confirm the write")
	}
	if err := fs.Parse(args); err != nil {
		return f, nil, err
	}
	return f, fs.Args(), nil
}

// baseURL resolves the server: --url, then --port on localhost, then the default port.
func (f connFlags) baseURL() string {
	if f.url != "" {
		return strings.TrimRight(f.url, "/")
	}
	port := defaultPort
	if f.port != 0 {
		port = f.port
	}
	return "http://localhost:" + strconv.Itoa(port)
}

// credentials resolves basic auth from flags, then the env vars `ocode serve`
// reads. A password without a username uses "opencode", as `ocode run -attach` does.
func (f connFlags) credentials() (string, string) {
	user, pass := f.username, f.password
	if pass == "" {
		pass = os.Getenv("OPENCODE_SERVER_PASSWORD")
	}
	if user == "" {
		user = os.Getenv("OPENCODE_SERVER_USERNAME")
	}
	if pass != "" && user == "" {
		user = "opencode"
	}
	return user, pass
}

// doRequest sends one JSON request and returns the body and status. A body of
// nil sends none. Encoding goes through json.Marshal, so any content is valid JSON.
func doRequest(method string, f connFlags, path string, body any) ([]byte, int, error) {
	var rdr io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("encode request: %w", err)
		}
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, f.baseURL()+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if user, pass := f.credentials(); user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("server at %s not reachable: %w", f.baseURL(), err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return out, resp.StatusCode, nil
}

// sessionPath escapes the id as one path segment, so an id cannot change the route.
func sessionPath(id, suffix string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("session_id is required")
	}
	return "/api/sessions/" + url.PathEscape(id) + suffix, nil
}

func httpError(code int, body []byte) error {
	return fmt.Errorf("server returned HTTP %d: %s", code, strings.TrimSpace(string(body)))
}

func statusCmd(args []string) error {
	f, rest, err := parseConnFlags("status", args, false)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("usage: ocode session status [flags] <session_id>")
	}
	path, err := sessionPath(rest[0], "/comm-status")
	if err != nil {
		return err
	}
	out, code, err := doRequest(http.MethodGet, f, path, nil)
	if err != nil {
		return err
	}
	if code >= 400 {
		return httpError(code, out)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		return fmt.Errorf("decode status: %w", err)
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// postCmd sends a message into another session. The Pulse assistant's
// session_send asks the operator first; this CLI has no such prompt, so the
// write needs an explicit --yes instead.
func postCmd(name string, args []string, queue bool) error {
	f, rest, err := parseConnFlags(name, args, true)
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("usage: ocode session %s [flags] --yes <session_id> <content...>", name)
	}
	id := rest[0]
	content := strings.Join(rest[1:], " ")
	if strings.TrimSpace(content) == "" {
		return errors.New("content is required")
	}
	if !f.yes {
		return fmt.Errorf("ocode session %s writes into session %s; re-run with --yes to confirm", name, id)
	}
	suffix, body := "/message", any(map[string]any{"content": content, "async": true})
	if queue {
		suffix, body = "/queue", map[string]any{"content": content}
	}
	path, err := sessionPath(id, suffix)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ocode session %s: writing to session %s (confirmed with --yes)\n", name, id)
	out, code, err := doRequest(http.MethodPost, f, path, body)
	if err != nil {
		return err
	}
	if code >= 400 {
		return httpError(code, out)
	}
	fmt.Println(strings.TrimSpace(string(out)))
	return nil
}
