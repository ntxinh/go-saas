package security_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ntxinh/go-saas/internal/shared/security"
)

func TestEncryptRoundTrip(t *testing.T) {
	c, err := security.New(strings.Repeat("ab", 32))
	require.NoError(t, err)

	blob, err := c.Encrypt("+491512345678")
	require.NoError(t, err)
	require.NotEmpty(t, blob)

	got, err := c.Decrypt(blob)
	require.NoError(t, err)
	assert.Equal(t, "+491512345678", got)
}

func TestEncryptNonceRandomized(t *testing.T) {
	c, err := security.New(strings.Repeat("ab", 32))
	require.NoError(t, err)

	a, err := c.Encrypt("same")
	require.NoError(t, err)
	b, err := c.Encrypt("same")
	require.NoError(t, err)
	assert.NotEqual(t, a, b, "GCM nonce must be random per encrypt")
}

func TestDecryptWrongKeyFails(t *testing.T) {
	c1, err := security.New(strings.Repeat("ab", 32))
	require.NoError(t, err)
	c2, err := security.New(strings.Repeat("cd", 32))
	require.NoError(t, err)

	blob, err := c1.Encrypt("secret")
	require.NoError(t, err)
	_, err = c2.Decrypt(blob)
	require.Error(t, err)
}

func TestDecryptTamperedFails(t *testing.T) {
	c, err := security.New(strings.Repeat("ab", 32))
	require.NoError(t, err)

	blob, err := c.Encrypt("secret")
	require.NoError(t, err)
	blob[len(blob)-1] ^= 0xff
	_, err = c.Decrypt(blob)
	require.Error(t, err)
}

func TestNewRejectsBadKey(t *testing.T) {
	_, err := security.New("not-hex")
	require.Error(t, err)
	_, err = security.New(strings.Repeat("ab", 16)) // 16 bytes, need 32
	require.Error(t, err)
}

func TestMayDecryptNilSafe(t *testing.T) {
	c, err := security.New(strings.Repeat("ab", 32))
	require.NoError(t, err)

	got, err := c.MayDecrypt(nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	blob, err := c.Encrypt("Ada")
	require.NoError(t, err)
	got, err = c.MayDecrypt(blob)
	require.NoError(t, err)
	assert.Equal(t, "Ada", got)

	var nilCipher *security.Cipher
	got, err = nilCipher.MayDecrypt(blob)
	require.NoError(t, err)
	assert.Empty(t, got)
}
