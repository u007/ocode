package sessioncli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

)

// Run is the entry point for `ocode session ...`.
func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ocode session <status|send|queue> <session_id> [content]")
	}
	switch args[0] {
	case "status":
		if len(args) < 2 {
			return fmt.Errorf("usage: ocode session status <session_id>")
		}
		return statusCmd(args[1])
	case "send":
		if len(args) < 3 {
			return fmt.Errorf("usage: ocode session send <session_id> <content>")
		}
		return postCmd(args[1], strings.Join(args[2:], " "), false)
	case "queue":
		if len(args) < 3 {
			return fmt.Errorf("usage: ocode session queue <session_id> <content>")
		}
		return postCmd(args[1], strings.Join(args[2:], " "), true)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func statusCmd(id string) error {
	// Try the local server first.
	resp, err := http.Get("http://localhost:4096/api/sessions/" + id + "/comm-status")
	if err == nil && resp.StatusCode == 200 {
		defer resp.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		b, _ := json.MarshalIndent(m, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	// Fallback: read from disk (no turn_active accuracy).
	// We don't know project root; scan common roots is out of scope, so report.
	fmt.Fprintln(os.Stderr, "server not reachable; status from disk unavailable without project root")
	return fmt.Errorf("no server on localhost:4096")
}

func postCmd(id, content string, queue bool) error {
	body := map[string]any{"content": content}
	if queue {
		body["queue"] = true
	}
	// If queue requested, use the queue endpoint when available; otherwise message endpoint.
	endpoint := "/api/sessions/" + id + "/message"
	if queue {
		endpoint = "/api/sessions/" + id + "/queue"
	}
	_ = body
	// Reuse message endpoint with async for non-queue
	data := []byte(fmt.Sprintf(`{"content":%q,"async":true}`, content))
	if queue {
		data = []byte(fmt.Sprintf(`{"content":%q}`, content))
	}
	req, _ := http.NewRequest("POST", "http://localhost:4096"+endpoint, strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	fmt.Printf("HTTP %d\n", resp.StatusCode)
	return nil
}
