package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPEmbedderParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer testkey" {
			t.Errorf("missing auth header")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"embedding": []float32{0.1, 0.2, 0.3}},
				{"embedding": []float32{0.4, 0.5, 0.6}},
			},
		})
	}))
	defer srv.Close()

	m := HTTPModel{ID: "test/model", Endpoint: srv.URL, Dimension: 3}
	e := NewHTTPEmbedder(m, "testkey")
	vecs, err := e.Embed(context.Background(), []string{"a", "b"}, Passage)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 3 || vecs[1][2] != 0.6 {
		t.Fatalf("bad parse: %+v", vecs)
	}
}

func TestHTTPModelByID(t *testing.T) {
	if _, ok := HTTPModelByID("openai/text-embedding-3-small"); !ok {
		t.Fatalf("expected openai small model in registry")
	}
}
