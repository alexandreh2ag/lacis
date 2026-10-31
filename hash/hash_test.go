package hash

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"
)

func TestPassword(t *testing.T) {
	encoded, err := Password("s3cret")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=4$"))

	other, err := Password("s3cret")
	require.NoError(t, err)
	assert.NotEqual(t, encoded, other, "each hash has its own salt")

	ok, err := VerifyPassword("s3cret", encoded)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = VerifyPassword("wrong", encoded)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestVerifyPassword_OtherParameters(t *testing.T) {
	// A hash made with weaker parameters, e.g. before they were raised.
	salt := []byte("somesalt")
	key := argon2.IDKey([]byte("password"), salt, 1, 4096, 1, 16)
	encoded := "$argon2id$v=19$m=4096,t=1,p=1$" + b64.EncodeToString(salt) + "$" + b64.EncodeToString(key)
	ok, err := VerifyPassword("password", encoded)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestVerifyPassword_Invalid(t *testing.T) {
	for _, encoded := range []string{
		"",
		"plain",
		"$bcrypt$v=19$m=4096,t=1,p=1$c29tZXNhbHQ$a2V5",
		"$argon2id$v=16$m=4096,t=1,p=1$c29tZXNhbHQ$a2V5",
		"$argon2id$v=x$m=4096,t=1,p=1$c29tZXNhbHQ$a2V5",
		"$argon2id$v=19$m=x$c29tZXNhbHQ$a2V5",
		"$argon2id$v=19$m=4096,t=1,p=1$!!!$a2V5",
		"$argon2id$v=19$m=4096,t=1,p=1$c29tZXNhbHQ$!!!",
		"$argon2id$v=19$m=4096,t=1,p=1$c29tZXNhbHQ$",
	} {
		ok, err := VerifyPassword("password", encoded)
		assert.ErrorIs(t, err, ErrInvalidHash, encoded)
		assert.False(t, ok)
	}
}

func TestNewToken(t *testing.T) {
	token, hashed, err := NewToken("lacis_et_")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(token, "lacis_et_"))
	assert.Len(t, token, len("lacis_et_")+43)
	assert.Equal(t, Token(token), hashed)
	assert.Len(t, hashed, 64)

	other, _, err := NewToken("lacis_et_")
	require.NoError(t, err)
	assert.NotEqual(t, token, other)
}

func TestToken(t *testing.T) {
	assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", Token(""))
}
