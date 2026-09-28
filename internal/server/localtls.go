package server

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"sync"
	"time"
)

// LocalCert is a per-launch self-signed certificate for the loopback origin.
// The desktop shell serves its webview over TLS with it so the webview
// negotiates HTTP/2: over plain HTTP every engine speaks HTTP/1.1 and caps a
// host at six connections, which long-lived SSE streams exhaust. The webview
// trusts this exact certificate by pinning (DER / SPKI hash), never through the
// system trust store.
type LocalCert struct {
	TLS tls.Certificate
	// DER is the leaf certificate (macOS pin compares it byte-for-byte).
	DER []byte
	// PEM is DER in PEM form (WebKitGTK builds a GTlsCertificate from it).
	PEM []byte
	// SPKISHA256 is base64(sha256(SubjectPublicKeyInfo)), the format Chromium's
	// --ignore-certificate-errors-spki-list expects (WebView2 pin).
	SPKISHA256 string
}

// NewLocalCert generates an ECDSA P-256 certificate valid for 127.0.0.1, ::1
// and localhost.
func NewLocalCert() (*LocalCert, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("local tls: generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("local tls: generate serial: %w", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "ocode local"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(0, 0, 30),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("local tls: create certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("local tls: parse certificate: %w", err)
	}
	spki := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	return &LocalCert{
		TLS:        tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf},
		DER:        der,
		PEM:        pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		SPKISHA256: base64.StdEncoding.EncodeToString(spki[:]),
	}, nil
}

// tlsConfig advertises h2 first so browsers multiplex over one connection.
// WebSocket upgrades still work: Go leaves HTTP/2 extended CONNECT disabled,
// so browsers open WebSockets on a separate HTTP/1.1 TLS connection.
func (c *LocalCert) tlsConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{c.TLS},
		NextProtos:   []string{"h2", "http/1.1"},
		MinVersion:   tls.VersionTLS12,
	}
}

// sniffTimeout bounds how long an accepted connection may take to send its
// first byte before it is dropped.
const sniffTimeout = 10 * time.Second

// tlsHandshakeRecord is the first byte of every TLS ClientHello.
const tlsHandshakeRecord = 0x16

// NewSniffTLSListener serves TLS and plain HTTP on one port: a connection whose
// first byte is a TLS handshake record is wrapped in tls.Server, anything else
// passes through as plain HTTP. Plain HTTP stays so LAN/tailscale share URLs,
// curl, htrcli and the TUI keep working against the same port.
//
// Each connection is sniffed on its own goroutine so a slow or silent client
// never blocks Accept for the others.
func NewSniffTLSListener(inner net.Listener, cert *LocalCert) net.Listener {
	l := &sniffListener{
		inner: inner,
		cfg:   cert.tlsConfig(),
		conns: make(chan net.Conn),
		done:  make(chan struct{}),
	}
	go l.acceptLoop()
	return l
}

type sniffListener struct {
	inner net.Listener
	cfg   *tls.Config
	conns chan net.Conn

	closeOnce sync.Once
	done      chan struct{}
	errMu     sync.Mutex
	err       error
}

func (l *sniffListener) acceptLoop() {
	for {
		c, err := l.inner.Accept()
		if ne, ok := err.(net.Error); ok && ne.Temporary() { //nolint:staticcheck // same retry rule net/http applies to Accept (e.g. EMFILE)
			log.Printf("serve: accept: %v; retrying", err)
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if err != nil {
			l.errMu.Lock()
			l.err = err
			l.errMu.Unlock()
			l.Close()
			return
		}
		go l.sniff(c)
	}
}

func (l *sniffListener) sniff(c net.Conn) {
	if err := c.SetReadDeadline(time.Now().Add(sniffTimeout)); err != nil {
		log.Printf("serve: sniff %s: set deadline: %v", c.RemoteAddr(), err)
		c.Close()
		return
	}
	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	if err != nil {
		// intentionally not logged: a client that connects and sends nothing
		// (port probes, aborted keep-alive dials) is routine.
		c.Close()
		return
	}
	if err := c.SetReadDeadline(time.Time{}); err != nil {
		log.Printf("serve: sniff %s: clear deadline: %v", c.RemoteAddr(), err)
		c.Close()
		return
	}
	var out net.Conn = &peekedConn{Conn: c, r: br}
	if first[0] == tlsHandshakeRecord {
		out = tls.Server(out, l.cfg)
	}
	select {
	case l.conns <- out:
	case <-l.done:
		out.Close()
	}
}

func (l *sniffListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		l.errMu.Lock()
		defer l.errMu.Unlock()
		if l.err != nil {
			return nil, l.err
		}
		return nil, net.ErrClosed
	}
}

func (l *sniffListener) Close() error {
	var err error
	l.closeOnce.Do(func() {
		close(l.done)
		err = l.inner.Close()
		if errors.Is(err, net.ErrClosed) {
			err = nil
		}
	})
	return err
}

func (l *sniffListener) Addr() net.Addr { return l.inner.Addr() }

// peekedConn replays the bytes buffered while sniffing.
type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
