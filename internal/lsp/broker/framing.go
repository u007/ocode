package broker

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const MaxFrameSize = 4 << 20

type Envelope struct {
	Protocol  int             `json:"protocol"`
	Kind      string          `json:"kind"`
	ClientID  string          `json:"client_id,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func WriteFrame(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxFrameSize {
		return fmt.Errorf("broker: frame exceeds %d bytes", MaxFrameSize)
	}
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], uint32(len(b)))
	if err = writeFull(w, h[:]); err != nil {
		return err
	}
	return writeFull(w, b)
}

func writeFull(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n > 0 {
			b = b[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
func ReadFrame(r io.Reader, v any) error {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(h[:])
	if n == 0 || n > MaxFrameSize {
		return fmt.Errorf("broker: invalid frame size %d", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
