// Package hash hashes the secrets the server stores: passwords with
// argon2id, tokens with SHA-256.
package hash

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters, the second recommendation of RFC 9106.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

var ErrInvalidHash = errors.New("invalid password hash")

// slots bounds the hashes computed at once to the number of CPUs: each
// takes argonMemory, so that parallel logins cannot exhaust the memory.
// The requests of a session hash nothing and never wait here.
var slots = make(chan struct{}, runtime.GOMAXPROCS(0))

// acquire takes a slot, or returns the error of ctx when it ends first;
// release gives the slot back.
func acquire(ctx context.Context) (release func(), err error) {
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

var b64 = base64.RawStdEncoding

// Password returns the argon2id hash of password in the PHC string format,
// e.g. $argon2id$v=19$m=65536,t=3,p=4$<salt>$<key>. It waits for a free
// slot, see slots.
func Password(ctx context.Context, password string) (string, error) {
	release, err := acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches encoded, a hash made by
// Password, possibly with other parameters. It waits for a free slot, see
// slots.
func VerifyPassword(ctx context.Context, password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrInvalidHash
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, ErrInvalidHash
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return false, ErrInvalidHash
	}
	release, err := acquire(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1, nil
}
