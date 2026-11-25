package main

import (
	"strings"
	"testing"
)

func TestSaveAckScriptDispatchesIDStepSeqAndOK(t *testing.T) {
	msg := msgSaveFileChunk + "abc:" + `{"id":"0f8c1e2a-3b4d-4e5f-8a9b-0c1d2e3f4a5b","seq":3,"data":"AAE="}`
	script, ok := saveAckScript(msg, true)
	if !ok {
		t.Fatalf("saveAckScript refused a well-formed chunk message")
	}
	want := `window.dispatchEvent(new CustomEvent("ocode:save-ack",{detail:{"id":"0f8c1e2a-3b4d-4e5f-8a9b-0c1d2e3f4a5b","step":"chunk","seq":3,"ok":true}}))`
	if script != want {
		t.Fatalf("script = %s\nwant    %s", script, want)
	}
}

func TestSaveAckScriptReportsRefusalAndStepForEndAndStart(t *testing.T) {
	start, ok := saveAckScript(msgSaveFileStart+"abc:"+`{"id":"sid-1","name":"a.pdf","size":4}`, false)
	if !ok || !strings.Contains(start, `"step":"start"`) || !strings.Contains(start, `"ok":false`) {
		t.Fatalf("start refusal ack = %q (ok=%v)", start, ok)
	}
	end, ok := saveAckScript(msgSaveFileEnd+"abc:"+`{"id":"sid-1"}`, true)
	if !ok || !strings.Contains(end, `"step":"end"`) || !strings.Contains(end, `"ok":true`) {
		t.Fatalf("end ack = %q (ok=%v)", end, ok)
	}
}

// A body with no usable session id gets no ack: the page then waits for its
// timeout instead of a dispatch that matches nothing.
func TestSaveAckScriptSkipsMessageWithoutSessionID(t *testing.T) {
	for _, msg := range []string{
		msgSaveFileStart + "abc:" + `{"name":"a.pdf"}`,
		msgSaveFileEnd + "abc:not json",
		msgSaveFileEnd + "abc-no-body",
	} {
		if script, ok := saveAckScript(msg, true); ok {
			t.Fatalf("saveAckScript(%q) built %q, want no ack", msg, script)
		}
	}
}
