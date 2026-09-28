package server

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// startSniffServer serves mux the way Server.Serve does with SetLocalTLS: a
// plain http.Server (no TLSConfig) over the sniffing listener.
func startSniffServer(t *testing.T, mux http.Handler) (addr string, cert *LocalCert) {
	t.Helper()
	cert, err := NewLocalCert()
	if err != nil {
		t.Fatalf("NewLocalCert: %v", err)
	}
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ln := NewSniffTLSListener(inner, cert)
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return inner.Addr().String(), cert
}

func pinnedClient(cert *LocalCert) *http.Client {
	pool := x509.NewCertPool()
	pool.AddCert(cert.TLS.Leaf)
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{RootCAs: pool},
			ForceAttemptHTTP2: true,
		},
	}
}

func TestSniffListenerServesPlainHTTPAndH2OnOnePort(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Proto)
	})
	addr, cert := startSniffServer(t, mux)

	plain, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + addr + "/ok")
	if err != nil {
		t.Fatalf("plain GET: %v", err)
	}
	body, _ := io.ReadAll(plain.Body)
	plain.Body.Close()
	if string(body) != "HTTP/1.1" {
		t.Fatalf("plain proto = %q, want HTTP/1.1", body)
	}

	resp, err := pinnedClient(cert).Get("https://" + addr + "/ok")
	if err != nil {
		t.Fatalf("tls GET: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.ProtoMajor != 2 || string(body) != "HTTP/2.0" {
		t.Fatalf("tls proto = %d / %q, want HTTP/2", resp.ProtoMajor, body)
	}
}

// A client that connects and never sends a byte must not stall Accept for
// everyone else.
func TestSniffListenerSilentClientDoesNotBlockOthers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	addr, _ := startSniffServer(t, mux)

	silent, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial silent: %v", err)
	}
	defer silent.Close()

	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + addr + "/ok")
	if err != nil {
		t.Fatalf("GET behind silent client: %v", err)
	}
	resp.Body.Close()
}

// Many concurrent long-lived streams share one h2 connection, so a further
// request still completes — the six-connection ceiling this exists to remove.
func TestSniffListenerH2MultiplexesPastSixStreams(t *testing.T) {
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: hi\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	addr, cert := startSniffServer(t, mux)
	defer close(release)

	client := pinnedClient(cert)
	client.Timeout = 0
	client.Transport.(*http.Transport).MaxConnsPerHost = 6
	for i := 0; i < 8; i++ {
		resp, err := client.Get("https://" + addr + "/events")
		if err != nil {
			t.Fatalf("stream %d: %v", i, err)
		}
		line, err := bufio.NewReader(resp.Body).ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "data: hi") {
			t.Fatalf("stream %d first line = %q, %v", i, line, err)
		}
		defer resp.Body.Close()
	}

	done := make(chan error, 1)
	go func() {
		resp, err := client.Get("https://" + addr + "/ok")
		if err == nil {
			resp.Body.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GET after 8 streams: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("GET after 8 open streams hung: requests are not multiplexed")
	}
}

// WebSockets need HTTP/1.1 Upgrade (Go leaves h2 extended CONNECT off), so a
// wss dial must still reach the handler through the TLS side.
func TestSniffListenerWebSocketOverTLS(t *testing.T) {
	up := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()
		mt, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		c.WriteMessage(mt, msg)
	})
	addr, cert := startSniffServer(t, mux)

	pool := x509.NewCertPool()
	pool.AddCert(cert.TLS.Leaf)
	d := websocket.Dialer{TLSClientConfig: &tls.Config{RootCAs: pool}, HandshakeTimeout: 5 * time.Second}
	c, _, err := d.Dial("wss://"+addr+"/ws", nil)
	if err != nil {
		t.Fatalf("wss dial: %v", err)
	}
	defer c.Close()
	if err := c.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, msg, err := c.ReadMessage()
	if err != nil || string(msg) != "ping" {
		t.Fatalf("echo = %q, %v", msg, err)
	}
}
