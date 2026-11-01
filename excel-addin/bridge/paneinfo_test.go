package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// newLocalBridge starts the bridge against ocodeURL with one workspace root for the workbooks it names.
func newLocalBridge(t *testing.T, ocodeURL, wsRoot string) *httptest.Server {
	t.Helper()
	h, _, err := NewHandler(Config{OcodeURL: ocodeURL, OcodeToken: "ocode-secret", PaneKey: key, CallTimeout: 2 * time.Second, WorkspaceRoot: wsRoot})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

const validPaneInfo = `{"fileName":"Budget.xlsx","path":"C:\\Work\\Budget.xlsx","host":"Excel","platform":"Windows","state":"saved",` +
	`"blank":false,"readOnly":null,"protectedView":false,"sheets":3}`

func postPaneInfo(t *testing.T, base, query, body string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/bridge/pane-info"+query, strings.NewReader(body))
	req.Header.Set("X-Bridge-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestPaneInfoValidation(t *testing.T) {
	s := newLocalBridge(t, "http://127.0.0.1:1", t.TempDir())
	openPaneStream(t, s.URL, "wb1", "pane-a")
	big := `{"fileName":"` + strings.Repeat("a", 17<<10) + `"}`
	cases := []struct {
		name string
		body string
		want int
	}{
		{"valid", validPaneInfo, http.StatusNoContent},
		{"blank null allowed", `{"fileName":"","path":"","host":"Excel","platform":"Mac","state":"new","blank":null,"readOnly":null,"protectedView":null,"sheets":0}`, http.StatusNoContent},
		{"unknown state", strings.Replace(validPaneInfo, `"saved"`, `"dirty"`, 1), http.StatusBadRequest},
		{"state is case sensitive", strings.Replace(validPaneInfo, `"saved"`, `"Saved"`, 1), http.StatusBadRequest},
		{"missing sheets", `{"fileName":"a","path":"","host":"","platform":"","state":"new","blank":null,"readOnly":null,"protectedView":null}`, http.StatusBadRequest},
		{"unknown key", strings.Replace(validPaneInfo, `"sheets":3`, `"sheets":3,"owner":"x"`, 1), http.StatusBadRequest},
		{"key in other case", strings.Replace(validPaneInfo, `"fileName"`, `"FILENAME"`, 1), http.StatusBadRequest},
		{"fileName is a number", strings.Replace(validPaneInfo, `"Budget.xlsx"`, `5`, 1), http.StatusBadRequest},
		{"fileName is null", strings.Replace(validPaneInfo, `"Budget.xlsx"`, `null`, 1), http.StatusBadRequest},
		{"sheets is a string", strings.Replace(validPaneInfo, `"sheets":3`, `"sheets":"3"`, 1), http.StatusBadRequest},
		{"sheets is fractional", strings.Replace(validPaneInfo, `"sheets":3`, `"sheets":2.5`, 1), http.StatusBadRequest},
		{"sheets is negative", strings.Replace(validPaneInfo, `"sheets":3`, `"sheets":-1`, 1), http.StatusBadRequest},
		{"blank is a string", strings.Replace(validPaneInfo, `"blank":false`, `"blank":"no"`, 1), http.StatusBadRequest},
		{"trailing data", validPaneInfo + " {}", http.StatusBadRequest},
		{"not an object", `[]`, http.StatusBadRequest},
		{"empty body", ``, http.StatusBadRequest},
		{"too large", big, http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := postPaneInfo(t, s.URL, "?workbook=wb1", c.body); got != c.want {
				t.Fatalf("status %d, want %d", got, c.want)
			}
		})
	}
	if got := postPaneInfo(t, s.URL, "?workbook=nope", validPaneInfo); got != http.StatusNotFound {
		t.Fatalf("unconnected workbook: status %d, want 404", got)
	}
	if got := postPaneInfo(t, s.URL, "", validPaneInfo); got != http.StatusBadRequest {
		t.Fatalf("missing workbook: status %d, want 400", got)
	}
	req, _ := http.NewRequest("POST", s.URL+"/bridge/pane-info?workbook=wb1", strings.NewReader(validPaneInfo))
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pane-info without the pane key: status %d, want 401", resp.StatusCode)
	}
}

func TestPaneInfoStampsLastSeenOnTheHub(t *testing.T) {
	h := NewHub()
	_, _, detach := h.Attach("wb", "pane-a")
	defer detach()
	first := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	info1 := &PaneInfo{FileName: "a.xlsx", State: "new"}
	info2 := &PaneInfo{FileName: "a.xlsx", State: "modified", Sheets: 2}
	if !h.SetPaneInfo("wb", info1, first) {
		t.Fatal("connected pane must accept info")
	}
	if got := h.Panes(); len(got) != 1 || got[0].lastSeen != first || got[0].info != info1 {
		t.Fatalf("after first post: %+v", got)
	}
	if !h.SetPaneInfo("wb", info2, second) {
		t.Fatal("second post refused")
	}
	got := h.Panes()
	if len(got) != 1 || got[0].lastSeen != second || got[0].info != info2 {
		t.Fatalf("after second post lastSeen/info not updated: %+v", got)
	}
	if got[0].connectedAt.IsZero() {
		t.Fatal("connectedAt must be set on attach")
	}
	if h.SetPaneInfo("other", info1, second) {
		t.Fatal("info for a workbook with no pane must be refused")
	}
}

func TestPaneDetachRemovesTheRecordAndItsInfo(t *testing.T) {
	h := NewHub()
	_, _, detach := h.Attach("wb", "pane-a")
	h.SetPaneInfo("wb", &PaneInfo{FileName: "a.xlsx", State: "saved"}, time.Now())
	detach()
	if got := h.Panes(); len(got) != 0 {
		t.Fatalf("detached pane still listed: %+v", got)
	}
	if h.SetPaneInfo("wb", &PaneInfo{FileName: "a.xlsx", State: "saved"}, time.Now()) {
		t.Fatal("post after detach must be refused")
	}
}

func TestStreamRejectsUnsafeWorkbookIDs(t *testing.T) {
	s := newLocalBridge(t, "http://127.0.0.1:1", t.TempDir())
	for _, wb := range []string{".", "..", "..%2Fx", "a%20b", "a%2Fb"} {
		req, _ := http.NewRequest("GET", s.URL+"/bridge/stream?workbook="+wb+"&instance=i", nil)
		req.Header.Set("X-Bridge-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("workbook %q: status %d, want 400", wb, resp.StatusCode)
		}
	}
}

// listedWorkbooks GETs /bridge/workbooks with the given query and decodes it.
func listedWorkbooks(t *testing.T, base, query string) (int, workbookListing) {
	t.Helper()
	req, _ := http.NewRequest("GET", base+"/bridge/workbooks"+query, nil)
	req.Header.Set("X-Bridge-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out workbookListing
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, out
}

func names(list []workbookEntry) []string {
	out := []string{}
	for _, e := range list {
		out = append(out, e.Workbook)
	}
	return out
}

func TestWorkbooksAreSortedAndPaginated(t *testing.T) {
	s := newLocalBridge(t, "http://127.0.0.1:1", t.TempDir())
	for _, wb := range []string{"w-b", "w-a2", "w-a1", "w-none"} {
		openPaneStream(t, s.URL, wb, "pane-"+wb)
	}
	for wb, body := range map[string]string{
		"w-b":  `{"fileName":"b.xlsx","path":"","host":"Excel","platform":"Mac","state":"new","blank":null,"readOnly":null,"protectedView":null,"sheets":1}`,
		"w-a2": `{"fileName":"a.xlsx","path":"","host":"Excel","platform":"Mac","state":"new","blank":null,"readOnly":null,"protectedView":null,"sheets":1}`,
		"w-a1": `{"fileName":"a.xlsx","path":"","host":"Excel","platform":"Mac","state":"new","blank":null,"readOnly":null,"protectedView":null,"sheets":1}`,
	} {
		if got := postPaneInfo(t, s.URL, "?workbook="+wb, body); got != http.StatusNoContent {
			t.Fatalf("pane-info for %s: %d", wb, got)
		}
	}

	status, all := listedWorkbooks(t, s.URL, "")
	if status != http.StatusOK || all.Total != 4 {
		t.Fatalf("status %d total %d", status, all.Total)
	}
	// Sorted by file name (the never-posted pane has none, so it sorts first), then workbook id.
	if got := names(all.Workbooks); strings.Join(got, ",") != "w-none,w-a1,w-a2,w-b" {
		t.Fatalf("order %v", got)
	}
	unposted := all.Workbooks[0]
	if unposted.State != "unknown" || unposted.FileName != "" || unposted.Blank != nil || unposted.Sheets != nil || unposted.LastSeen != "" {
		t.Fatalf("never-posted pane must show unknown fields, got %+v", unposted)
	}
	if unposted.ConnectedAt == "" || unposted.Sessions == nil || len(unposted.Sessions) != 0 {
		t.Fatalf("never-posted pane: connectedAt %q sessions %#v", unposted.ConnectedAt, unposted.Sessions)
	}

	pages := []struct {
		query string
		want  string
	}{
		{"?limit=2", "w-none,w-a1"},
		{"?limit=2&offset=2", "w-a2,w-b"},
		{"?offset=3", "w-b"},
		{"?offset=10", ""},
		{"", "w-none,w-a1,w-a2,w-b"},
	}
	for _, p := range pages {
		status, out := listedWorkbooks(t, s.URL, p.query)
		if status != http.StatusOK || out.Total != 4 || strings.Join(names(out.Workbooks), ",") != p.want {
			t.Fatalf("query %q: status %d total %d workbooks %v, want %q", p.query, status, out.Total, names(out.Workbooks), p.want)
		}
	}

	for _, q := range []string{"?limit=0", "?limit=201", "?limit=x", "?offset=-1", "?offset=y"} {
		if status, _ := listedWorkbooks(t, s.URL, q); status != http.StatusBadRequest {
			t.Fatalf("query %q: status %d, want 400", q, status)
		}
	}
	if status, out := listedWorkbooks(t, s.URL, "?limit=200"); status != http.StatusOK || out.Total != 4 {
		t.Fatalf("limit 200 is the maximum and must be accepted: %d", status)
	}
}

func TestWorkbooksShapeSessionsAndWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(root+"/w1", 0o700); err != nil {
		t.Fatal(err)
	}
	s := newLocalBridge(t, "http://127.0.0.1:1", root)
	openPaneStream(t, s.URL, "w1", "pane-w1")
	openPaneStream(t, s.URL, "w2", "pane-w2")
	if got := postPaneInfo(t, s.URL, "?workbook=w1", validPaneInfo); got != http.StatusNoContent {
		t.Fatalf("pane-info: %d", got)
	}
	var refs []sessionRef
	for i := 0; i < 25; i++ {
		refs = append(refs, sessionRef{ID: "ses_" + string(rune('a'+i)), Title: "[Excel] Budget.xlsx", Created: "2026-11-01T10:00:00Z"})
	}
	putState(t, s.URL, "w1", "sessions", refs)

	req, _ := http.NewRequest("GET", s.URL+"/bridge/workbooks", nil)
	req.Header.Set("X-Bridge-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw struct {
		Total     int                          `json:"total"`
		Workbooks []map[string]json.RawMessage `json:"workbooks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if raw.Total != 2 || len(raw.Workbooks) != 2 {
		t.Fatalf("total %d, %d entries", raw.Total, len(raw.Workbooks))
	}
	wantKeys := []string{"workbook", "fileName", "path", "host", "platform", "state", "blank", "readOnly", "protectedView", "sheets", "connectedAt", "lastSeen", "workspace", "sessions"}
	for _, e := range raw.Workbooks {
		for _, k := range wantKeys {
			if _, ok := e[k]; !ok {
				t.Fatalf("entry %v lacks %q", e, k)
			}
		}
	}

	_, out := listedWorkbooks(t, s.URL, "")
	byID := map[string]workbookEntry{}
	for _, e := range out.Workbooks {
		byID[e.Workbook] = e
	}
	w1 := byID["w1"]
	if w1.Workspace != root+"/w1" || w1.State != "saved" || w1.FileName != "Budget.xlsx" || w1.LastSeen == "" {
		t.Fatalf("w1: %+v", w1)
	}
	if len(w1.Sessions) != 20 || w1.Sessions[0] != "ses_a" || w1.Sessions[19] != "ses_t" {
		t.Fatalf("w1 sessions must be the first 20 stored ids: %v", w1.Sessions)
	}
	w2 := byID["w2"]
	if w2.Workspace != "" || w2.Sessions == nil || len(w2.Sessions) != 0 {
		t.Fatalf("w2 has no workspace folder and no index: %+v", w2)
	}
}

func TestWorkbooksRequireKeyAndLoopback(t *testing.T) {
	s := newLocalBridge(t, "http://127.0.0.1:1", t.TempDir())
	resp, err := http.Get(s.URL + "/bridge/workbooks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without the pane key: status %d, want 401", resp.StatusCode)
	}

	h, _, err := NewHandler(Config{OcodeURL: "http://127.0.0.1:1", PaneKey: key, WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/bridge/workbooks", nil)
	req.RemoteAddr = "10.1.2.3:5555"
	req.Header.Set("X-Bridge-Key", key)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-loopback caller: status %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "workbooks") {
		t.Fatalf("refused caller got listing text: %q", rec.Body.String())
	}
}
