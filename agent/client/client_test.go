package client

import (
	"compress/gzip"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/common/api"
	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/common/version"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newServer serves handler over TLS under /lacis and returns a client
// trusting its certificate.
func newServer(t *testing.T, handler http.HandlerFunc) Client {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	fs := afero.NewMemMapFs()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	require.NoError(t, afero.WriteFile(fs, "/etc/lacis/ca.pem", ca, 0o600))
	c, err := New(config.ServerConfig{URL: srv.URL + "/lacis/", CAFile: "/etc/lacis/ca.pem"}, fs)
	require.NoError(t, err)
	return c
}

func TestEnroll(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/lacis/"+api.PathEnroll, r.URL.Path)
		assert.Equal(t, "Bearer lacis_et_secret", r.Header.Get(api.HeaderAuthorization))
		assert.Equal(t, version.Version, r.Header.Get(api.HeaderAgentVersion))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"agent_id":"019a","agent_token":"lacis_at_secret"}`))
	})
	got, err := c.Enroll(t.Context(), "lacis_et_secret")
	require.NoError(t, err)
	assert.Equal(t, &api.EnrollResponse{AgentID: "019a", AgentToken: "lacis_at_secret"}, got)
}

func TestEnroll_Errors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr *Error
		wantMsg string
	}{
		{
			name:    "api error",
			status:  http.StatusUnauthorized,
			body:    `{"code":"invalid_token","message":"unknown, expired or revoked enrollment token"}`,
			wantErr: &Error{Status: 401, Code: api.ErrCodeInvalidToken, Message: "unknown, expired or revoked enrollment token"},
			wantMsg: "server answered 401 invalid_token: unknown, expired or revoked enrollment token",
		},
		{
			name:    "not an api error",
			status:  http.StatusBadGateway,
			body:    "<html>bad gateway</html>\n",
			wantErr: &Error{Status: 502, Message: "<html>bad gateway</html>"},
			wantMsg: "server answered 502: <html>bad gateway</html>",
		},
		{
			name:    "long body",
			status:  http.StatusNotFound,
			body:    strings.Repeat("x", 300),
			wantErr: &Error{Status: 404, Message: strings.Repeat("x", 200) + "…"},
		},
		{name: "empty body", status: http.StatusNotFound, wantErr: &Error{Status: 404}, wantMsg: "server answered 404"},
		{name: "invalid answer", status: http.StatusCreated, body: "{", wantMsg: "decode " + api.PathEnroll},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			_, err := c.Enroll(t.Context(), "lacis_et_secret")
			require.Error(t, err)
			if tt.wantErr != nil {
				assert.Equal(t, tt.wantErr, err)
			}
			if tt.wantMsg != "" {
				assert.ErrorContains(t, err, tt.wantMsg)
			}
		})
	}
}

func TestEnroll_Unreachable(t *testing.T) {
	c, err := New(config.ServerConfig{URL: "https://127.0.0.1:1"}, afero.NewMemMapFs())
	require.NoError(t, err)
	_, err = c.Enroll(t.Context(), "lacis_et_secret")
	assert.ErrorContains(t, err, "connection refused")
}

func TestEnroll_UntrustedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	c, err := New(config.ServerConfig{URL: srv.URL}, afero.NewMemMapFs())
	require.NoError(t, err)
	_, err = c.Enroll(t.Context(), "lacis_et_secret")
	assert.ErrorContains(t, err, "certificate")
}

func TestNew_Errors(t *testing.T) {
	_, err := New(config.ServerConfig{URL: "https://lacis.example.com", CAFile: "/missing.pem"}, afero.NewMemMapFs())
	assert.ErrorContains(t, err, "read CA file")

	c := &client{baseURL: "://bad/", http: http.DefaultClient}
	_, err = c.Enroll(t.Context(), "lacis_et_secret")
	assert.Error(t, err)
}

func TestSendSnapshot(t *testing.T) {
	s := snapshot.New(snapshot.AgentInfo{Version: "1.2.0"}, labels.Labels{Namespace: "acme", Project: "site", Env: "prod"}, "acme-site-prod-1")
	tests := []struct {
		name   string
		status int
		body   string
		want   api.SnapshotStatus
	}{
		{name: "accepted", status: http.StatusAccepted, body: `{"status":"accepted"}`, want: api.SnapshotAccepted},
		{name: "unchanged", status: http.StatusOK, body: `{"status":"unchanged"}`, want: api.SnapshotUnchanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/lacis/"+api.PathSnapshot, r.URL.Path)
				assert.Equal(t, "Bearer lacis_at_secret", r.Header.Get(api.HeaderAuthorization))
				assert.Equal(t, api.EncodingGzip, r.Header.Get(api.HeaderContentEncoding))
				assert.Equal(t, api.ContentTypeJSON, r.Header.Get(api.HeaderContentType))
				assert.Equal(t, "1", r.Header.Get(api.HeaderSchemaVersion))
				gz, err := gzip.NewReader(r.Body)
				require.NoError(t, err)
				got := &snapshot.Snapshot{}
				require.NoError(t, json.NewDecoder(gz).Decode(got))
				assert.Equal(t, "acme-site-prod-1", got.Host.Hostname)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			got, err := c.SendSnapshot(t.Context(), "lacis_at_secret", s)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("refused", func(t *testing.T) {
		c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"namespace_mismatch","message":"moved"}`))
		})
		_, err := c.SendSnapshot(t.Context(), "lacis_at_secret", s)
		assert.Equal(t, &Error{Status: 403, Code: api.ErrCodeNamespaceMismatch, Message: "moved"}, err)
	})
	t.Run("invalid request", func(t *testing.T) {
		c := &client{baseURL: "://bad/", http: http.DefaultClient}
		_, err := c.SendSnapshot(t.Context(), "lacis_at_secret", s)
		assert.Error(t, err)
	})
}
