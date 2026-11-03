package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefusal(t *testing.T) {
	err := refuse(ErrInvalid, ReasonHostNotIn, map[string]any{"host": "web-1", "network": "lan"}, "%s is not in %s", "web-1", "lan")
	assert.EqualError(t, err, "invalid input: web-1 is not in lan")
	assert.ErrorIs(t, err, ErrInvalid)
	var r *Refusal
	require.True(t, errors.As(err, &r))
	assert.Equal(t, ReasonHostNotIn, r.Reason)
	assert.Equal(t, map[string]any{"host": "web-1", "network": "lan"}, r.Args)
}

func TestRefusal_Reasons(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		reason string
		args   map[string]any
	}{
		{"username", func() error { _, err := Username("-bad"); return err }(), ReasonInvalidUsername, nil},
		{"password", func() error { _, err := passwordHash(context.Background(), "short"); return err }(), ReasonPasswordTooShort, map[string]any{"min": MinPasswordLength}},
		{"oidc group", func() error { _, err := oidcGroups([]string{" "}); return err }(), ReasonInvalidOIDCGroup, map[string]any{"max": maxGroup}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r *Refusal
			require.True(t, errors.As(tt.err, &r), "%v", tt.err)
			assert.ErrorIs(t, tt.err, ErrInvalid)
			assert.Equal(t, tt.reason, r.Reason)
			assert.Equal(t, tt.args, r.Args)
		})
	}
}
