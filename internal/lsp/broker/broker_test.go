package broker

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFrameRoundTripAndLimit(t *testing.T) {
	var b bytes.Buffer
	in := Envelope{Protocol: ProtocolVersion, Kind: "hello", ClientID: "c1"}
	if err := WriteFrame(&b, in); err != nil {
		t.Fatal(err)
	}
	var out Envelope
	if err := ReadFrame(&b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip mismatch: %#v != %#v", out, in)
	}
	if err := WriteFrame(&b, bytes.Repeat([]byte{'x'}, MaxFrameSize)); err == nil {
		t.Fatal("expected oversized frame error")
	}
}

type shortWriter struct{ b bytes.Buffer }

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return w.b.Write(p)
}

func TestWriteFrameHandlesShortWrites(t *testing.T) {
	var w shortWriter
	if err := WriteFrame(&w, Envelope{Protocol: ProtocolVersion, Kind: "x"}); err != nil {
		t.Fatal(err)
	}
	var got Envelope
	if err := ReadFrame(&w.b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "x" {
		t.Fatalf("kind=%q", got.Kind)
	}
}

func TestMetadataRejectsInvalidEndpoint(t *testing.T) {
	i := Identity{Root: "/project", Executable: "/bin/gopls", LanguageID: "go"}
	m, err := NewMetadata(i, 0, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadata(filepath.Join(t.TempDir(), "m.json"), m); err == nil {
		t.Fatal("expected invalid endpoint")
	}
}

func TestCanonicalRootResolvesSymlink(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "project-link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err := CanonicalRoot(link)
	if err != nil {
		t.Fatal(err)
	}
	want, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("canonical root=%q want %q", got, want)
	}
}

func TestAuthenticatedLoopbackHandshake(t *testing.T) {
	m, err := NewMetadata(Identity{Root: "/project", Executable: "/bin/gopls", LanguageID: "go"}, 0, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	connected := make(chan net.Conn, 1)
	l, err := Listen(m, func(c net.Conn) { connected <- c })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	conn, err := Connect(context.Background(), l.Metadata(), "client", 3, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case c := <-connected:
		c.Close()
	case <-time.After(time.Second):
		t.Fatal("handshake callback not invoked")
	}

	bad := l.Metadata()
	bad.Token = "wrong"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := Connect(ctx, bad, "client", 2, time.Millisecond); err == nil {
		t.Fatal("expected authentication failure")
	}
}

func TestConnectIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	_, err := Connect(ctx, Metadata{Protocol: ProtocolVersion, Port: 1, Token: "x"}, "c", 3, time.Millisecond)
	if err == nil || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("unbounded connect: err=%v", err)
	}
}

func TestStartOnceCallsStartOnlyAfterExistingRejects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.json")
	called := 0
	m, err := StartOnce(path, func(Metadata) error { return errors.New("dead") }, func() (Metadata, error) {
		called++
		return NewMetadata(Identity{Root: "/project", Executable: "/bin/gopls", LanguageID: "go"}, 42, os.Getpid())
	})
	if err != nil || called != 1 || m.Port != 42 {
		t.Fatalf("start result: m=%#v called=%d err=%v", m, called, err)
	}
	_, err = StartOnce(path, func(Metadata) error { return nil }, func() (Metadata, error) { called++; return Metadata{}, nil })
	if err != nil || called != 1 {
		t.Fatalf("duplicate start: called=%d err=%v", called, err)
	}
}

func TestMetadataAtomicWriteAndOwnerDelete(t *testing.T) {
	i := Identity{Root: "/project", Executable: "/bin/gopls", LanguageID: "go"}
	m, err := NewMetadata(i, 1234, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "meta.json")
	if err := WriteMetadata(path, m); err != nil {
		t.Fatal(err)
	}
	got, err := ReadMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.BrokerID != m.BrokerID || got.Token == "" {
		t.Fatalf("metadata mismatch: %#v", got)
	}
	if err := RemoveMetadataIfOwner(path, "other"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err := RemoveMetadataIfOwner(path, m.BrokerID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("metadata still exists: %v", err)
	}
}

func TestCanonicalRoot(t *testing.T) {
	d := t.TempDir()
	got, err := CanonicalRoot(d)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(d)
	if got != filepath.Clean(want) {
		t.Fatalf("got %q want %q", got, want)
	}
}
