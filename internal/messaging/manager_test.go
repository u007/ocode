package messaging

import (
	"context"
	"path/filepath"
	"testing"
)

type fakeProv struct{ sent []string }

func (f *fakeProv) Name() string { return "fake" }
func (f *fakeProv) Start(ctx context.Context, acc Account, store *Store, onInbound InboundHandler) error {
	return nil
}
func (f *fakeProv) Stop() {}
func (f *fakeProv) Send(ctx context.Context, chatID, text string) (string, error) {
	f.sent = append(f.sent, chatID+":"+text)
	return "msg-1", nil
}
func (f *fakeProv) Status() string { return "connected" }

func TestManagerSend(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(filepath.Join(dir, "m.db"))
	defer st.Close()
	_ = st.UpsertAccount(Account{ID: "a1", Provider: ProviderTelegram, Label: "bot"})
	mgr := NewManager(st, dir)
	fp := &fakeProv{}
	mgr.providers["a1"] = fp
	mid, err := mgr.Send(context.Background(), "a1", "chat1", "hi")
	if err != nil || mid != "msg-1" {
		t.Fatalf("send: %v %s", err, mid)
	}
	msgs, _ := st.ListMessages("a1", "chat1", 10)
	if len(msgs) != 1 || msgs[0].Direction != "out" {
		t.Fatalf("outbound not persisted: %+v", msgs)
	}
}
