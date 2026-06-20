package discovery

import (
	"context"
	"testing"
)

func TestFakeEmbedderDeterministic(t *testing.T) {
	fe := FakeEmbedder{Dimension: 64}
	a, err := fe.Embed(context.Background(), []string{"hello world"}, Passage)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := fe.Embed(context.Background(), []string{"hello world"}, Passage)
	if len(a) != 1 || len(a[0]) != 64 {
		t.Fatalf("want 1x64, got %dx%d", len(a), len(a[0]))
	}
	if Cosine(a[0], b[0]) < 0.999 {
		t.Fatalf("same text must embed identically")
	}
}

func TestFakeEmbedderDiscriminates(t *testing.T) {
	fe := FakeEmbedder{Dimension: 128}
	v, _ := fe.Embed(context.Background(),
		[]string{"send email to the team", "send email", "compile rust binary"}, Passage)
	near := Cosine(v[0], v[1])  // related
	far := Cosine(v[0], v[2])   // unrelated
	if near <= far {
		t.Fatalf("related texts must score higher: near=%.3f far=%.3f", near, far)
	}
}
