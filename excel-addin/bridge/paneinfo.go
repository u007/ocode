package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

// maxPaneInfoBytes caps one POST /bridge/pane-info body. A real report is a few hundred bytes.
const maxPaneInfoBytes = 16 << 10

// paneStates are the save states a pane may report.
var paneStates = map[string]bool{"new": true, "saved": true, "modified": true, "unknown": true}

// paneInfoKeys is the exact key set of a pane-info body. Every key must be present.
var paneInfoKeys = []string{"fileName", "path", "host", "platform", "state", "blank", "readOnly", "protectedView", "sheets"}

// paneInfoNullable are the keys a pane may send as null because it could not tell.
var paneInfoNullable = map[string]bool{"blank": true, "readOnly": true, "protectedView": true}

// PaneInfo is what the task pane reports about its workbook (POST /bridge/pane-info).
type PaneInfo struct {
	FileName      string `json:"fileName"`
	Path          string `json:"path"`
	Host          string `json:"host"`
	Platform      string `json:"platform"`
	State         string `json:"state"`
	Blank         *bool  `json:"blank"`
	ReadOnly      *bool  `json:"readOnly"`
	ProtectedView *bool  `json:"protectedView"`
	Sheets        int    `json:"sheets"`
}

// decodePaneInfo checks a pane-info body strictly: the key set must be exact, non-nullable keys must not be null,
// every value must have its type, and state and sheets must be in range.
func decodePaneInfo(body []byte) (PaneInfo, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return PaneInfo{}, fmt.Errorf("pane-info body must be a JSON object: %w", err)
	}
	for _, k := range paneInfoKeys {
		v, ok := raw[k]
		if !ok {
			return PaneInfo{}, fmt.Errorf("pane-info is missing %q", k)
		}
		if !paneInfoNullable[k] && string(v) == "null" {
			return PaneInfo{}, fmt.Errorf("pane-info %q must not be null", k)
		}
	}
	if len(raw) != len(paneInfoKeys) {
		return PaneInfo{}, errors.New("pane-info has a field that is not in the schema")
	}
	var info PaneInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return PaneInfo{}, fmt.Errorf("pane-info has a field of the wrong type: %w", err)
	}
	if !paneStates[info.State] {
		return PaneInfo{}, fmt.Errorf("pane-info state %q is not new, saved, modified or unknown", info.State)
	}
	if info.Sheets < 0 {
		return PaneInfo{}, fmt.Errorf("pane-info sheets %d is negative", info.Sheets)
	}
	return info, nil
}

// paneInfo records what the task pane reports about its workbook (POST /bridge/pane-info?workbook=<id>). The record
// lives on the pane, so it goes when the pane detaches.
func (s *srv) paneInfo(w http.ResponseWriter, r *http.Request) {
	wb := r.URL.Query().Get("workbook")
	if wb == "" {
		http.Error(w, "workbook is required", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPaneInfoBytes))
	if err != nil {
		http.Error(w, "pane-info body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	info, err := decodePaneInfo(body)
	if err != nil {
		bridgeRouteError(w, http.StatusBadRequest, err)
		return
	}
	if !s.hub.SetPaneInfo(wb, &info, time.Now()) {
		http.Error(w, "no Excel task pane is connected for this workbook", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// workbookListing is the body of GET /bridge/workbooks and of the list_workbooks tool.
type workbookListing struct {
	Total     int             `json:"total"`
	Workbooks []workbookEntry `json:"workbooks"`
}

// workbookEntry is one connected workbook. A field the pane never reported is empty, or null for the pointers.
type workbookEntry struct {
	Workbook      string   `json:"workbook"`
	FileName      string   `json:"fileName"`
	Path          string   `json:"path"`
	Host          string   `json:"host"`
	Platform      string   `json:"platform"`
	State         string   `json:"state"`
	Blank         *bool    `json:"blank"`
	ReadOnly      *bool    `json:"readOnly"`
	ProtectedView *bool    `json:"protectedView"`
	Sheets        *int     `json:"sheets"`
	ConnectedAt   string   `json:"connectedAt"`
	LastSeen      string   `json:"lastSeen"`
	Workspace     string   `json:"workspace"`
	Sessions      []string `json:"sessions"`
}

// sessionsKey is the state key the pane's session index lives under (identity.ts, SessionIndex).
const sessionsKey = "sessions"

// sessionListCap bounds the session ids reported per workbook.
const sessionListCap = 20

// sessionRef is one entry of the pane's session index (identity.ts, SessionRef). The pane writes newest first.
type sessionRef struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Created string `json:"created"`
}

// workbooks answers GET /bridge/workbooks?limit=&offset=. It is loopback-only, like /mcp.
func (s *srv) workbooks(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := listPageQuery(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.listWorkbooks(limit, offset)
	if err != nil {
		bridgeRouteError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// listPageQuery reads the limit and offset query parameters of the workbook listing.
func listPageQuery(q url.Values) (int, int, error) {
	limit, err := optionalInt("limit", q.Get("limit"))
	if err != nil {
		return 0, 0, err
	}
	offset, err := optionalInt("offset", q.Get("offset"))
	if err != nil {
		return 0, 0, err
	}
	return pageArgs(limit, offset, 50, 200)
}

// optionalInt parses an optional integer parameter. An empty value is absent (nil).
func optionalInt(name, v string) (*int, error) {
	if v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer, got %q", name, v)
	}
	return &n, nil
}

// pageArgs applies the default and the bounds to a limit and an offset. A nil offset means 0.
func pageArgs(limit, offset *int, defLimit, maxLimit int) (int, int, error) {
	l, o := defLimit, 0
	if limit != nil {
		if *limit < 1 || *limit > maxLimit {
			return 0, 0, fmt.Errorf("limit must be between 1 and %d", maxLimit)
		}
		l = *limit
	}
	if offset != nil {
		if *offset < 0 {
			return 0, 0, errors.New("offset must be 0 or more")
		}
		o = *offset
	}
	return l, o, nil
}

// listWorkbooks sorts the connected panes by file name, then workbook id, and fills only the requested page.
func (s *srv) listWorkbooks(limit, offset int) (workbookListing, error) {
	panes := s.hub.Panes()
	sort.Slice(panes, func(i, j int) bool {
		a, b := fileNameOf(panes[i]), fileNameOf(panes[j])
		if a != b {
			return a < b
		}
		return panes[i].workbook < panes[j].workbook
	})
	total := len(panes)
	start := min(offset, total)
	end := min(start+limit, total)
	out := workbookListing{Total: total, Workbooks: make([]workbookEntry, 0, end-start)}
	for _, p := range panes[start:end] {
		e, err := s.workbookEntry(p)
		if err != nil {
			return workbookListing{}, err
		}
		out.Workbooks = append(out.Workbooks, e)
	}
	return out, nil
}

func fileNameOf(p paneRecord) string {
	if p.info == nil {
		return ""
	}
	return p.info.FileName
}

// workbookEntry builds one row from a pane record, its workspace folder and its session index.
func (s *srv) workbookEntry(p paneRecord) (workbookEntry, error) {
	e := workbookEntry{Workbook: p.workbook, State: "unknown", ConnectedAt: stamp(p.connectedAt), LastSeen: stamp(p.lastSeen), Sessions: []string{}}
	if p.info != nil {
		e.FileName, e.Path, e.Host, e.Platform, e.State = p.info.FileName, p.info.Path, p.info.Host, p.info.Platform, p.info.State
		e.Blank, e.ReadOnly, e.ProtectedView = p.info.Blank, p.info.ReadOnly, p.info.ProtectedView
		sheets := p.info.Sheets
		e.Sheets = &sheets
	}
	dir, err := s.workspaceDir(p.workbook)
	if err != nil {
		return workbookEntry{}, err
	}
	e.Workspace = dir
	refs, err := s.workbookSessions(p.workbook)
	if err != nil {
		return workbookEntry{}, err
	}
	for _, r := range refs {
		if len(e.Sessions) == sessionListCap {
			break
		}
		e.Sessions = append(e.Sessions, r.ID)
	}
	return e, nil
}

// stamp formats a time for the listing; a zero time (never happened) is the empty string.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// workspaceDir is the workbook's workspace folder (created by the pane on load), or "" when it does not exist yet or
// no workspace root is known. The caller decides what "" means.
func (s *srv) workspaceDir(wb string) (string, error) {
	if s.workspaceRoot == "" {
		return "", nil
	}
	dir := filepath.Join(s.workspaceRoot, wb)
	fi, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("checking workspace %s: %w", dir, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("workspace %s is not a directory", dir)
	}
	return dir, nil
}

// workbookSessions returns the pane's session index for a workbook as stored (newest first). A missing index is empty.
func (s *srv) workbookSessions(wb string) ([]sessionRef, error) {
	b, found, err := s.state.get(wb, sessionsKey)
	if err != nil {
		return nil, fmt.Errorf("reading the session index of workbook %s: %w", wb, err)
	}
	if !found {
		return nil, nil
	}
	var refs []sessionRef
	if err := json.Unmarshal(b, &refs); err != nil {
		return nil, fmt.Errorf("the session index of workbook %s is not a list of sessions: %w", wb, err)
	}
	return refs, nil
}

// maxSessionRefBytes caps one POST /bridge/sessions/{workbook} body. One entry is a few hundred bytes.
const maxSessionRefBytes = 4 << 10

// maxSessionTitleChars bounds a title in the session index.
const maxSessionTitleChars = 200

// sessionsLock returns the lock that serializes every writer of one workbook's session index: the PUT of the sessions
// key and POST /bridge/sessions/{workbook}. Each writer reads the index and then rewrites it, so they must not interleave.
func (s *srv) sessionsLock(wb string) *sync.Mutex {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	l, ok := s.sessionLocks[wb]
	if !ok {
		l = &sync.Mutex{}
		s.sessionLocks[wb] = l
	}
	return l
}

// addSession appends ref to a workbook's session index under the index lock. The index is newest first, as the pane
// writes it. An id already in the index leaves the index unchanged. It returns the index as stored.
func (s *srv) addSession(wb string, ref sessionRef) ([]sessionRef, error) {
	lock := s.sessionsLock(wb)
	lock.Lock()
	defer lock.Unlock()
	refs, err := s.workbookSessions(wb)
	if err != nil {
		return nil, err
	}
	if slices.ContainsFunc(refs, func(r sessionRef) bool { return r.ID == ref.ID }) {
		return refs, nil
	}
	refs = append([]sessionRef{ref}, refs...)
	b, err := json.Marshal(refs)
	if err != nil {
		return nil, fmt.Errorf("encoding the session index of workbook %s: %w", wb, err)
	}
	if err := s.state.put(wb, sessionsKey, b); err != nil {
		return nil, fmt.Errorf("writing the session index of workbook %s: %w", wb, err)
	}
	return refs, nil
}

// checkSessionRef validates one index entry. The id reaches a URL path later, so it must match apiSessionIDRe. The title
// may be empty but is bounded. created must be an RFC 3339 instant.
func checkSessionRef(ref sessionRef) error {
	if !apiSessionIDRe.MatchString(ref.ID) {
		return fmt.Errorf("id %q is not a valid ocode session id", ref.ID)
	}
	if n := utf8.RuneCountInString(ref.Title); n > maxSessionTitleChars {
		return fmt.Errorf("title is %d characters, the limit is %d", n, maxSessionTitleChars)
	}
	if _, err := time.Parse(time.RFC3339, ref.Created); err != nil {
		return fmt.Errorf("created must be an RFC 3339 time: %w", err)
	}
	return nil
}

// postSession answers POST /bridge/sessions/{workbook}: it registers one chat in the workbook's session index and
// returns the whole index. Re-posting a registered id changes nothing.
func (s *srv) postSession(w http.ResponseWriter, r *http.Request) {
	wb := r.PathValue("workbook")
	if !validWorkbookName(wb) {
		http.Error(w, "invalid workbook", http.StatusBadRequest)
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSessionRefBytes))
	if err != nil {
		http.Error(w, "body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	var ref sessionRef
	if err := decodeArgs(b, &ref); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := checkSessionRef(ref); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	refs, err := s.addSession(wb, ref)
	if err != nil {
		bridgeRouteError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, refs)
}
