package hash

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

const tokenBytes = 32

// NewToken returns a random token starting with prefix, and its hash to
// store. Only the hash is kept: the token is shown once.
func NewToken(prefix string) (token, hashed string, err error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = prefix + base64.RawURLEncoding.EncodeToString(b)
	return token, Token(token), nil
}

// Token returns the hash under which token is stored. A token is random
// and long, so a fast hash is enough and allows looking it up.
func Token(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
