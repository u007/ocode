package server

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/stt"
)

// Rejection paths only: they return before any config write or model call, so
// the tests never touch the real HOME or spawn Python.
func TestSetSTTRejectsUnknownModel(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodPut, "/api/stt", strings.NewReader(`{"model":"not-a-model"}`))
	w := httptest.NewRecorder()
	s.handleSetSTT(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestSetSTTRejectsTrailingJSON(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodPut, "/api/stt", strings.NewReader(`{"model":"whisper-1"} {}`))
	w := httptest.NewRecorder()
	s.handleSetSTT(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestTranscribeSTTRequiresMultipartAudio(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", strings.NewReader(`{"text":"x"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTranscribeSTT(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-multipart status = %d, want 400", w.Code)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormField("other")
	_, _ = fw.Write([]byte("x"))
	_ = mw.Close()
	r = httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w = httptest.NewRecorder()
	s.handleTranscribeSTT(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing audio field status = %d, want 400", w.Code)
	}
}

// TestPrewarmSTTAnswersImmediately checks that the prewarm endpoint answers
// without waiting for the model load. The whole point is to overlap the load
// with the user speaking, so blocking here would defeat it.
//
// The seam matters: without it the prewarm fails immediately (the server test
// binary redirects HOME, so the model is not where the worker expects it),
// and a handler that loaded inline would still answer fast — the test would
// pass without proving anything. The seam makes the load slow enough that an
// inline handler is caught.
func TestPrewarmSTTAnswersImmediately(t *testing.T) {
	orig := sttPrewarmFunc
	t.Cleanup(func() { sttPrewarmFunc = orig })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	sttPrewarmFunc = func(ctx context.Context, opts stt.Options) error {
		<-release
		return nil
	}

	s := &Server{}
	r := httptest.NewRequest(http.MethodPost, "/api/stt/prewarm", nil)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		s.handlePrewarmSTT(w, r)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the prewarm endpoint blocked; it must answer before the model load finishes")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// TestPrewarmSTTSurvivesABurst checks that a spammed prewarm endpoint stays
// responsive.
//
// The handler must not hold a server lock and must not wait for the model
// load, or a burst of requests (a user double-clicking the microphone, or a
// script hammering the endpoint) would serialise behind one slow load. Every
// request has to answer promptly even while other prewarms are in flight.
//
// The worker-level idempotence (one process, one model load, no matter how
// many prewarms arrive) is covered by the stt package's
// TestPrewarmIsIdempotent.
func TestPrewarmSTTSurvivesABurst(t *testing.T) {
	orig := sttPrewarmFunc
	t.Cleanup(func() { sttPrewarmFunc = orig })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	sttPrewarmFunc = func(ctx context.Context, opts stt.Options) error {
		<-release
		return nil
	}

	s := &Server{}
	const requests = 8
	var wg sync.WaitGroup
	errs := make(chan int, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest(http.MethodPost, "/api/stt/prewarm", nil)
			w := httptest.NewRecorder()
			s.handlePrewarmSTT(w, r)
			errs <- w.Code
		}()
	}
	// Every request must answer while the loads are still blocked.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a burst of prewarm requests did not all answer; the handler must not wait for the model load")
	}
	close(errs)
	for code := range errs {
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
	}
}

// TestPrewarmSTTNeverApprovesAnInstall checks that the prewarm endpoint
// cannot install anything.
//
// The install-approval flow belongs to the transcription path: it answers
// HTTP 409 so the UI can ask the user. A prewarm that set InstallApproved
// would silently run pip on the user's machine with no prompt, which is
// exactly the surprise the install-approval contract exists to prevent.
//
// The handler's options are captured through the seam, so the assertion is
// on the options the handler actually passed.
func TestPrewarmSTTNeverApprovesAnInstall(t *testing.T) {
	orig := sttPrewarmFunc
	t.Cleanup(func() { sttPrewarmFunc = orig })
	var got stt.Options
	var mu sync.Mutex
	sttPrewarmFunc = func(ctx context.Context, opts stt.Options) error {
		mu.Lock()
		got = opts
		mu.Unlock()
		return nil
	}

	s := &Server{}
	r := httptest.NewRequest(http.MethodPost, "/api/stt/prewarm", nil)
	w := httptest.NewRecorder()
	s.handlePrewarmSTT(w, r)

	// The prewarm runs in the background, so wait for it to record.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		seen := got
		mu.Unlock()
		if seen.OpenAIKey != nil {
			if seen.InstallApproved {
				t.Fatal("the prewarm endpoint approved a dependency install; " +
					"the install prompt belongs to the transcription path")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the prewarm never ran")
}

// TestCancelPrewarmSTTReportsTheSeam checks that the cancel endpoint delegates to
// stt.CancelPrewarm and reports its result, so the web recorder can stop a load
// that is still in progress when the recording is discarded.
func TestCancelPrewarmSTTReportsTheSeam(t *testing.T) {
	orig := sttCancelPrewarmFunc
	t.Cleanup(func() { sttCancelPrewarmFunc = orig })
	sttCancelPrewarmFunc = func() bool { return true }

	s := &Server{}
	r := httptest.NewRequest(http.MethodPost, "/api/stt/prewarm/cancel", nil)
	w := httptest.NewRecorder()
	s.handleCancelPrewarmSTT(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"cancelled":true`) {
		t.Fatalf("body = %s, want cancelled:true", body)
	}
}
