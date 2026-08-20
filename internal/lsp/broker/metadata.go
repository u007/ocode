package broker

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/u007/ocode/internal/filelock"
)

const ProtocolVersion = 1

type Metadata struct {
	Protocol  int       `json:"protocol"`
	Identity  Identity  `json:"identity"`
	Port      int       `json:"port"`
	PID       int       `json:"pid"`
	BrokerID  string    `json:"broker_id"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

func NewMetadata(i Identity, port, pid int) (Metadata, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return Metadata{}, err
	}
	id := hex.EncodeToString(b)
	return Metadata{Protocol: ProtocolVersion, Identity: i, Port: port, PID: pid, BrokerID: id, Token: id, CreatedAt: time.Now().UTC()}, nil
}
func ReadMetadata(path string) (Metadata, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, err
	}
	var m Metadata
	if err := json.Unmarshal(b, &m); err != nil {
		return Metadata{}, fmt.Errorf("broker metadata: %w", err)
	}
	if err := validateMetadata(m); err != nil {
		return Metadata{}, err
	}
	return m, nil
}

func validateMetadata(m Metadata) error {
	if m.Protocol != ProtocolVersion {
		return fmt.Errorf("broker metadata: unsupported protocol %d", m.Protocol)
	}
	if m.Identity.Root == "" || m.Identity.Executable == "" || m.Identity.LanguageID == "" {
		return fmt.Errorf("broker metadata: incomplete identity")
	}
	if m.Port < 1 || m.Port > 65535 || m.PID < 1 || m.BrokerID == "" || m.Token == "" {
		return fmt.Errorf("broker metadata: invalid endpoint or owner")
	}
	return nil
}
func WriteMetadata(path string, m Metadata) error {
	if err := validateMetadata(m); err != nil {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".metadata-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	// The rename is atomic; sync the containing directory so the publication
	// survives a host crash as well as a process crash.
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func WithStartupLock(metadataPath string, fn func() error) error {
	return filelock.WithFileLock(metadataPath+".lock", fn)
}
func RemoveMetadataIfOwner(path, brokerID string) error {
	return WithStartupLock(path, func() error {
		m, err := ReadMetadata(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if m.BrokerID == brokerID {
			return os.Remove(path)
		}
		return nil
	})
}
