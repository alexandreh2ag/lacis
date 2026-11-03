package resolver_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/alexandreh2ag/lacis/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// extensions are the extensions of the first error of the response.
func extensions(t *testing.T, f *fixture, query string) map[string]any {
	t.Helper()
	var errs []struct {
		Extensions map[string]any `json:"extensions"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.errors(t, query)), &errs))
	require.NotEmpty(t, errs)
	return errs[0].Extensions
}

func TestRefusalReasons(t *testing.T) {
	update := `mutation { updateUser(id:"` + id(1).String() + `", input:{password:"x"}) { username } }`
	tests := []struct {
		name string
		err  error
		want map[string]any
	}{
		{
			name: "reason and args",
			err:  &service.Refusal{Err: service.ErrInvalid, Reason: service.ReasonPasswordTooShort, Args: map[string]any{"min": 8}, Detail: "short"},
			want: map[string]any{"code": "BAD_USER_INPUT", "reason": "PASSWORD_TOO_SHORT", "args": map[string]any{"min": float64(8)}},
		},
		{
			name: "wrapped, without args",
			err:  fmt.Errorf("update: %w", &service.Refusal{Err: service.ErrAlreadyExists, Reason: service.ReasonUserExists, Detail: "user bob"}),
			want: map[string]any{"code": "CONFLICT", "reason": "USER_EXISTS"},
		},
		{
			name: "last admin",
			err:  service.ErrLastAdmin,
			want: map[string]any{"code": "CONFLICT", "reason": "LAST_ADMIN"},
		},
		{
			name: "no reason",
			err:  service.ErrInvalid,
			want: map[string]any{"code": "BAD_USER_INPUT"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, admin())
			f.users.EXPECT().Update(gomock.Any(), gomock.Any(), id(1), gomock.Any()).Return(nil, tt.err)
			assert.Equal(t, tt.want, extensions(t, f, update))
		})
	}
}

func TestHistory_HostNotFound(t *testing.T) {
	f := newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	f.history.EXPECT().List(gomock.Any(), "acme", gomock.Any()).Return(nil,
		&service.Refusal{Err: service.ErrHostNotFound, Reason: service.ReasonHostNotFound, Args: map[string]any{"hostname": "web-9"}, Detail: "web-9"})
	assert.Equal(t, map[string]any{"code": "NOT_FOUND", "reason": "HOST_NOT_FOUND", "args": map[string]any{"hostname": "web-9"}},
		extensions(t, f, `{ namespace(slug:"acme") { history(filter:{hostname:"web-9"}) { hasMore } } }`))
}
