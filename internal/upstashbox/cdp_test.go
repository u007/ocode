package upstashbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestCDPURLConnectUsesSixtySecondTimeout checks the connect call's deadline,
// since Chromium starts on first use.
func TestCDPURLConnectUsesSixtySecondTimeout(t *testing.T) {
	var deadline time.Time
	var hasDeadline bool
	c := newStubClient(t, func(r *http.Request) (*http.Response, error) {
		deadline, hasDeadline = r.Context().Deadline()
		return jsonResponse(r, http.StatusOK, `{"cdp_url":"wss://`+cdpHost+`/?token=`+testToken+`"}`), nil
	})
	if _, err := c.CDPURL(context.Background(), "box-1"); err != nil {
		t.Fatalf("CDPURL: %v", err)
	}
	if !hasDeadline {
		t.Fatal("connect call has no deadline")
	}
	if left := time.Until(deadline); left <= 59*time.Second || left > connectCallTimeout {
		t.Errorf("deadline is %v away, want about %v", left, connectCallTimeout)
	}
}

const cdpHost = "sunny-guppy-35079-9223.preview.box.upstash.com"

func TestCDPURLRequestAndRedaction(t *testing.T) {
	raw := "wss://" + cdpHost + "?token=" + testToken
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		expectRequest(t, r, http.MethodPost, "/v2/box/sunny-guppy-35079/browser/connect")
		body, _ := json.Marshal(map[string]string{"cdp_url": raw})
		_, _ = w.Write(body)
	})
	u, err := c.CDPURL(context.Background(), "sunny-guppy-35079")
	if err != nil {
		t.Fatalf("CDPURL: %v", err)
	}

	printed := []string{
		u.String(),
		u.GoString(),
		fmt.Sprintf("%v", u),
		fmt.Sprintf("%+v", u),
		fmt.Sprintf("%#v", u),
		fmt.Sprintf("%s", u),
	}
	for _, s := range printed {
		if strings.Contains(s, testToken) {
			t.Errorf("printed form leaks the token: %s", s)
		}
	}
	if !strings.Contains(u.String(), cdpHost) {
		t.Errorf("String() should keep the host: %s", u.String())
	}
	if got := u.RevealRaw(); got != raw {
		t.Errorf("RevealRaw() = %q, want the full URL", got)
	}
}

func TestCDPURLZeroValueIsSafe(t *testing.T) {
	var u CDPURL
	if got := u.String(); strings.Contains(got, "://") || got == "" {
		t.Errorf("zero String() = %q", got)
	}
	if got := u.RevealRaw(); got != "" {
		t.Errorf("zero RevealRaw() = %q", got)
	}
}

// TestCDPURLBadShapesDoNotLeakToken checks that every rejection, including a
// URL that fails to parse, keeps the token out of the error.
func TestCDPURLBadShapesDoNotLeakToken(t *testing.T) {
	cases := map[string]string{
		"missing cdp_url": `{}`,
		"empty cdp_url":   `{"cdp_url":""}`,
		"camelCase key":   `{"cdpUrl":"wss://` + cdpHost + `/?token=` + testToken + `"}`,
		"https scheme":    `{"cdp_url":"https://` + cdpHost + `/?token=` + testToken + `"}`,
		"no token":        `{"cdp_url":"wss://` + cdpHost + `/"}`,
		"unparseable url": `{"cdp_url":"wss://[bad?token=` + testToken + `"}`,
		"empty token":     `{"cdp_url":"wss://` + cdpHost + `/?token="}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			})
			_, err := c.CDPURL(context.Background(), "box-1")
			if err == nil {
				t.Fatal("CDPURL: want error")
			}
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("error leaks the token: %v", err)
			}
		})
	}
}
