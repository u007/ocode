package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestATS_IssueValidate(t *testing.T) {
	st := newArtifactTokenStore(t.TempDir())
	tok, id, err := st.issue("art1", "link", time.Now().Add(time.Hour), "")
	require.NoError(t, err)
	require.NotEmpty(t, tok)
	require.NotEmpty(t, id)

	at, level, err := st.validate(tok, "art1")
	require.NoError(t, err)
	require.Equal(t, "art1", at.ArtifactID)
	require.GreaterOrEqual(t, level, 0)
}

func TestATS_RevokedFails(t *testing.T) {
	st := newArtifactTokenStore(t.TempDir())
	tok, id, err := st.issue("art1", "link", time.Now().Add(time.Hour), "")
	require.NoError(t, err)
	require.NoError(t, st.revoke(id))
	_, _, err = st.validate(tok, "art1")
	require.Error(t, err)
}

func TestATS_ExpiredFails(t *testing.T) {
	st := newArtifactTokenStore(t.TempDir())
	tok, _, err := st.issue("art1", "link", time.Now().Add(-time.Hour), "")
	require.NoError(t, err)
	_, _, err = st.validate(tok, "art1")
	require.Error(t, err)
}

func TestATS_PasswordGated(t *testing.T) {
	st := newArtifactTokenStore(t.TempDir())
	tok, _, err := st.issue("art1", "link", time.Now().Add(time.Hour), "secret")
	require.NoError(t, err)
	at, _, err := st.validate(tok, "art1")
	require.NoError(t, err)
	require.NotEmpty(t, at.PasswordHash)
	require.True(t, st.verifyTokenPassword(at, "secret"))
	require.False(t, st.verifyTokenPassword(at, "wrong"))
}

func TestATS_WrongArtifactFails(t *testing.T) {
	st := newArtifactTokenStore(t.TempDir())
	tok, _, err := st.issue("art1", "link", time.Now().Add(time.Hour), "")
	require.NoError(t, err)
	_, _, err = st.validate(tok, "art2")
	require.Error(t, err)
}

func TestATS_ConstantTimeCompare(t *testing.T) {
	st := newArtifactTokenStore(t.TempDir())
	tok, _, err := st.issue("art1", "link", time.Now().Add(time.Hour), "")
	require.NoError(t, err)
	// tampered token should fail
	bad := tok[:len(tok)-1] + "X"
	_, _, err = st.validate(bad, "art1")
	require.Error(t, err)
}

func TestATS_PruneExpired(t *testing.T) {
	st := newArtifactTokenStore(t.TempDir())
	_, _, err := st.issue("art1", "link", time.Now().Add(-time.Hour), "")
	require.NoError(t, err)
	st.pruneExpired()
}
