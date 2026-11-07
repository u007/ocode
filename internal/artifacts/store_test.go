package artifacts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStoreCreateAndFetchArtifact(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	a, err := s.CreateArtifact(Artifact{Name: "demo", Status: "active"})
	require.NoError(t, err)
	got, err := s.GetArtifact(a.ID)
	require.NoError(t, err)
	require.Equal(t, "demo", got.Name)
}

func TestStoreListPaginatedSorted(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	_, _ = s.CreateArtifact(Artifact{Name: "a", Status: "active"})
	_, _ = s.CreateArtifact(Artifact{Name: "b", Status: "active"})
	list, err := s.ListArtifacts(1, 0)
	require.NoError(t, err)
	require.Len(t, list, 1)
	list2, err := s.ListArtifacts(10, 0)
	require.NoError(t, err)
	require.Len(t, list2, 2)
}

func TestStorePublishVersionAndFiles(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	a, _ := s.CreateArtifact(Artifact{Name: "demo", Status: "active"})
	v, err := s.PublishVersion(Version{ArtifactID: a.ID, Version: "1", Entrypoint: "index.html"}, []File{{Path: "index.html", BlobHash: "abc", Size: 5}})
	require.NoError(t, err)
	got, err := s.GetVersion(v.ID)
	require.NoError(t, err)
	require.Equal(t, a.ID, got.ArtifactID)
	vs, err := s.ListVersions(a.ID)
	require.NoError(t, err)
	require.Len(t, vs, 1)
}

func TestStoreTokenMintRevoke(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	a, _ := s.CreateArtifact(Artifact{Name: "demo", Status: "active"})
	tok, err := s.MintToken(AccessToken{ArtifactID: a.ID, TokenHash: "hash1", Kind: "read"})
	require.NoError(t, err)
	got, err := s.GetTokenByHash("hash1")
	require.NoError(t, err)
	require.Equal(t, tok.ID, got.ID)
	require.NoError(t, s.RevokeToken(tok.ID))
	got2, err := s.GetTokenByHash("hash1")
	require.NoError(t, err)
	require.False(t, got2.RevokedAt.IsZero())
}

func TestStoreAllowEntryAndInvite(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	a, _ := s.CreateArtifact(Artifact{Name: "demo", Status: "active"})
	err = s.AddAllowEntry(AllowEntry{ArtifactID: a.ID, Email: "x@y.com", InviteTokenHash: "invhash"})
	require.NoError(t, err)
	got, err := s.GetAllowEntry(a.ID, "x@y.com")
	require.NoError(t, err)
	require.Equal(t, "x@y.com", got.Email)
	acc, err := s.AcceptInvite("invhash")
	require.NoError(t, err)
	require.False(t, acc.AcceptedAt.IsZero())
	require.Equal(t, "x@y.com", acc.Email)
}

func TestStoreLogAccess(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	a, _ := s.CreateArtifact(Artifact{Name: "demo", Status: "active"})
	err = s.LogAccess(AccessLogEntry{ArtifactID: a.ID, Status: 200})
	require.NoError(t, err)
}

func TestStoreGCRemovesUnreferencedBlob(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	bs, _ := NewBlobStore(dir)
	h1, _ := bs.Put([]byte("referenced"))
	h2, _ := bs.Put([]byte("unreferenced"))
	a, _ := s.CreateArtifact(Artifact{Name: "demo", Status: "active"})
	_, _ = s.PublishVersion(Version{ArtifactID: a.ID}, []File{{Path: "f", BlobHash: h1}})
	require.NoError(t, s.GC())
	ok, _ := bs.Has(h1)
	require.True(t, ok)
	ok, _ = bs.Has(h2)
	require.False(t, ok)
}

func TestInviteTTLOverride(t *testing.T) {
	t.Setenv("OCODE_ARTIFACT_INVITE_TTL_DAYS", "")
	require.Equal(t, InviteTTL, inviteTTL())

	t.Setenv("OCODE_ARTIFACT_INVITE_TTL_DAYS", "3")
	require.Equal(t, 3*24*time.Hour, inviteTTL())

	for _, bad := range []string{"0", "-2", "abc", "  "} {
		t.Setenv("OCODE_ARTIFACT_INVITE_TTL_DAYS", bad)
		require.Equal(t, InviteTTL, inviteTTL(), "bad value %q must fall back", bad)
	}
}
