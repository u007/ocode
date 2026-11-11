package messaging

import (
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(filepath.Join(dir, "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	acc := Account{ID: "tg-1", Provider: ProviderTelegram, Label: "bot", Credentials: "token", Status: "connected"}
	if err := st.UpsertAccount(acc); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetAccount("tg-1")
	if err != nil || got.Label != "bot" {
		t.Fatalf("get: %v %v", err, got)
	}
	_ = st.AddMessage(Message{ID: "m1", AccountID: "tg-1", ChatID: "c1", Text: "hello world", Direction: "in"})
	res, err := st.SearchMessages("hello", "tg-1", 10)
	if err != nil || len(res) != 1 {
		t.Fatalf("search: %v len=%d", err, len(res))
	}
}

func TestStoreMultiAccountIsolation(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(filepath.Join(dir, "m.db"))
	defer st.Close()
	_ = st.UpsertAccount(Account{ID: "a1", Provider: ProviderTelegram, Label: "a"})
	_ = st.UpsertAccount(Account{ID: "a2", Provider: ProviderWhatsApp, Label: "b"})
	_ = st.AddMessage(Message{ID: "m1", AccountID: "a1", ChatID: "c", Text: "secret", Direction: "in"})
	_ = st.AddMessage(Message{ID: "m2", AccountID: "a2", ChatID: "c", Text: "other", Direction: "in"})
	msgs, _ := st.ListMessages("a1", "c", 10)
	if len(msgs) != 1 || msgs[0].Text != "secret" {
		t.Fatalf("isolation broken: %+v", msgs)
	}
	// dedup on re-deliver
	_ = st.AddMessage(Message{ID: "m1", AccountID: "a1", ChatID: "c", Text: "secret2", Direction: "in"})
	msgs, _ = st.ListMessages("a1", "c", 10)
	if len(msgs) != 1 || msgs[0].Text != "secret2" {
		t.Fatalf("dedup failed: %+v", msgs)
	}
}

func TestStoreFTSSearch(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(filepath.Join(dir, "m.db"))
	defer st.Close()
	_ = st.AddMessage(Message{ID: "m1", AccountID: "a", ChatID: "c", Text: "the quick brown fox", Direction: "in", SenderName: "alice"})
	_ = st.AddMessage(Message{ID: "m2", AccountID: "a", ChatID: "c", Text: "lazy dog", Direction: "in", SenderName: "bob"})
	res, err := st.SearchMessages("fox", "", 10)
	if err != nil || len(res) != 1 {
		t.Fatalf("fts search: %v len=%d", err, len(res))
	}
}

func TestStoreExportAndCursor(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir + "/m.db")
	defer st.Close()
	_ = st.AddMessage(Message{ID: "m1", AccountID: "a", ChatID: "c", Text: "hi", Direction: "in"})
	msgs, cur, err := st.ExportChanges(0, 10)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("export: %v len=%d", err, len(msgs))
	}
	if cur == 0 {
		t.Fatal("cursor not advanced")
	}
	if err := st.SetCursor("push", cur); err != nil {
		t.Fatal(err)
	}
	if st.GetCursor("push") != cur {
		t.Fatal("cursor mismatch")
	}
	// import idempotent
	if err := st.ImportMessages(msgs); err != nil {
		t.Fatal(err)
	}
	all, _ := st.ListMessages("a", "c", 10)
	if len(all) != 1 {
		t.Fatalf("import dup: %d", len(all))
	}
}
