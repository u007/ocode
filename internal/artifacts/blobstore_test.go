package artifacts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlobStorePutGetHas(t *testing.T) {
	bs, err := NewBlobStore(t.TempDir())
	require.NoError(t, err)
	data := []byte("hello world")
	h, err := bs.Put(data)
	require.NoError(t, err)
	require.Len(t, h, 64)
	got, err := bs.Get(h)
	require.NoError(t, err)
	require.Equal(t, data, got)
	ok, err := bs.Has(h)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = bs.Has("0000000000000000000000000000000000000000000000000000000000000000")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestBlobStoreGCUnreferenced(t *testing.T) {
	dir := t.TempDir()
	bs, err := NewBlobStore(dir)
	require.NoError(t, err)
	h1, _ := bs.Put([]byte("a"))
	h2, _ := bs.Put([]byte("b"))
	err = bs.GCUnreferenced(map[string]bool{h1: true})
	require.NoError(t, err)
	ok, _ := bs.Has(h1)
	require.True(t, ok)
	ok, _ = bs.Has(h2)
	require.False(t, ok)
}
