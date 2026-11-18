package backup

import (
	"bytes"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pass = "correct horse battery staple"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	var sealed bytes.Buffer
	require.NoError(t, Encrypt(&sealed, strings.NewReader("archive bytes"), pass))
	assert.True(t, IsEncrypted(sealed.Bytes()))
	assert.NotContains(t, sealed.String(), "archive bytes")

	var plain bytes.Buffer
	require.NoError(t, Decrypt(&plain, bytes.NewReader(sealed.Bytes()), pass))
	assert.Equal(t, "archive bytes", plain.String())

	err := Decrypt(&bytes.Buffer{}, bytes.NewReader(sealed.Bytes()), "a wrong passphrase!")
	assert.ErrorIs(t, err, ErrWrongPassphrase)
	err = Decrypt(&bytes.Buffer{}, strings.NewReader("not age"), pass)
	assert.ErrorIs(t, err, ErrInvalidBackup)

	assert.ErrorIs(t, Encrypt(&bytes.Buffer{}, strings.NewReader("x"), "short"), ErrWeakPassphrase)
	assert.False(t, IsEncrypted([]byte{0x1f, 0x8b, 0x08}))
}

// A file sealed with the age tool's own default work factor opens (a user
// may re-encrypt a copy by hand), but not one that asks for far more.
func TestDecryptWorkFactorLimit(t *testing.T) {
	seal := func(factor int) []byte {
		r, err := age.NewScryptRecipient(pass)
		require.NoError(t, err)
		r.SetWorkFactor(factor)
		var b bytes.Buffer
		w, err := age.Encrypt(&b, r)
		require.NoError(t, err)
		_, _ = w.Write([]byte("x"))
		require.NoError(t, w.Close())
		return b.Bytes()
	}
	require.NoError(t, Decrypt(&bytes.Buffer{}, bytes.NewReader(seal(18)), pass))
	err := Decrypt(&bytes.Buffer{}, bytes.NewReader(seal(maxWorkFactor+1)), pass)
	assert.Error(t, err)
}
