package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	helloKind = "hello"
	ackKind   = "hello_ack"
)

type handshake struct {
	Token    string   `json:"token"`
	BrokerID string   `json:"broker_id"`
	Identity Identity `json:"identity"`
}

type Listener struct {
	ln     net.Listener
	meta   Metadata
	closed chan struct{}
	onConn func(net.Conn)
	once   sync.Once
}

// Listen starts an authenticated loopback broker listener.
func Listen(meta Metadata, onConn func(net.Conn)) (*Listener, error) {
	if meta.Protocol != ProtocolVersion {
		return nil, fmt.Errorf("broker: unsupported protocol %d", meta.Protocol)
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	meta.Port = ln.Addr().(*net.TCPAddr).Port
	l := &Listener{ln: ln, meta: meta, closed: make(chan struct{}), onConn: onConn}
	go l.acceptLoop()
	return l, nil
}

func (l *Listener) Metadata() Metadata { return l.meta }

func (l *Listener) acceptLoop() {
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			select {
			case <-l.closed:
				return
			default:
			}
			continue
		}
		go l.authenticate(conn)
	}
}

func (l *Listener) authenticate(conn net.Conn) {
	defer conn.SetDeadline(time.Time{})
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	var env Envelope
	if err := ReadFrame(conn, &env); err != nil || env.Protocol != ProtocolVersion || env.Kind != helloKind {
		_ = conn.Close()
		return
	}
	var h handshake
	if json.Unmarshal(env.Payload, &h) != nil || h.Token == "" || h.Token != l.meta.Token || h.BrokerID != l.meta.BrokerID || h.Identity.Hash() != l.meta.Identity.Hash() {
		_ = conn.Close()
		return
	}
	if err := WriteFrame(conn, Envelope{Protocol: ProtocolVersion, Kind: ackKind, ClientID: env.ClientID, Payload: mustJSON(l.meta.BrokerID)}); err != nil {
		_ = conn.Close()
		return
	}
	if l.onConn != nil {
		l.onConn(conn)
	} else {
		_ = conn.Close()
	}
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func (l *Listener) Close() error {
	var err error
	l.once.Do(func() { close(l.closed); err = l.ln.Close() })
	return err
}

// Connect authenticates to a broker, retrying only until ctx expires or the
// bounded attempt count is exhausted. It never starts a broker itself.
func Connect(ctx context.Context, meta Metadata, clientID string, attempts int, delay time.Duration) (net.Conn, error) {
	if attempts < 1 {
		return nil, errors.New("broker: attempts must be positive")
	}
	if meta.Protocol != ProtocolVersion || meta.Port < 1 || meta.Token == "" {
		return nil, errors.New("broker: invalid metadata")
	}
	var last error
	for i := 0; i < attempts; i++ {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", fmt.Sprintf("127.0.0.1:%d", meta.Port))
		if err == nil {
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
			payload, _ := json.Marshal(handshake{Token: meta.Token, BrokerID: meta.BrokerID, Identity: meta.Identity})
			if err = WriteFrame(conn, Envelope{Protocol: ProtocolVersion, Kind: helloKind, ClientID: clientID, Payload: payload}); err == nil {
				var ack Envelope
				err = ReadFrame(conn, &ack)
				var brokerID string
				if len(ack.Payload) > 0 {
					_ = json.Unmarshal(ack.Payload, &brokerID)
				}
				if err == nil && ack.Protocol == ProtocolVersion && ack.Kind == ackKind && brokerID == meta.BrokerID {
					_ = conn.SetDeadline(time.Time{})
					return conn, nil
				}
			}
			_ = conn.Close()
		}
		last = err
		if i+1 < attempts {
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, ctx.Err()
			case <-t.C:
			}
		}
	}
	if last == nil {
		last = io.ErrUnexpectedEOF
	}
	return nil, fmt.Errorf("broker: connect: %w", last)
}

// StartOnce serializes discovery/startup. Existing metadata is tried first;
// start is called at most once while holding the startup lock.
func StartOnce(metadataPath string, existing func(Metadata) error, start func() (Metadata, error)) (Metadata, error) {
	// The lock file lives next to metadataPath, whose directory (per-project,
	// under paths.GlobalDataDir()) does not exist until the first daemon for
	// that project starts. filelock.WithFileLock only O_CREATEs the lock
	// file itself, not its parent, so the very first call for a new project
	// would otherwise fail before ever reaching the lock.
	if err := os.MkdirAll(filepath.Dir(metadataPath), 0700); err != nil {
		return Metadata{}, fmt.Errorf("broker: create metadata dir: %w", err)
	}
	var result Metadata
	err := WithStartupLock(metadataPath, func() error {
		if m, err := ReadMetadata(metadataPath); err == nil {
			if existing != nil {
				if err = existing(m); err == nil {
					result = m
					return nil
				}
			}
			_ = os.Remove(metadataPath)
		} else if !os.IsNotExist(err) {
			return err
		}
		m, err := start()
		if err != nil {
			return err
		}
		if err = WriteMetadata(metadataPath, m); err != nil {
			return err
		}
		result = m
		return nil
	})
	return result, err
}
