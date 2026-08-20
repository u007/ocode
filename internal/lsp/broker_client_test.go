package lsp

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/u007/ocode/internal/lsp/broker"
)

type brokerClientTestUpstream struct{}

func (brokerClientTestUpstream) Call(method string, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(method + "-reply")
}
func (brokerClientTestUpstream) Notify(string, json.RawMessage) error { return nil }

func TestNewBrokerClientRoutesThroughAuthenticatedBroker(t *testing.T) {
	identity := broker.Identity{Root: "/tmp/project", Executable: "/bin/fake", LanguageID: "go"}
	meta, err := broker.NewMetadata(identity, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := broker.Listen(meta, func(conn net.Conn) {
		broker.ServeRPC(conn, brokerClientTestUpstream{}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	client, err := NewBrokerClient(context.Background(), listener.Metadata(), "lsp-client")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	result, err := client.Call("workspace/symbol", map[string]string{"query": "x"})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if got != "workspace/symbol-reply" {
		t.Fatalf("result = %q", got)
	}
}
