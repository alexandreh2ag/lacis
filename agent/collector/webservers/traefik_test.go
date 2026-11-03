package webservers

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraefikAPI(t *testing.T) {
	followed := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			user, password, ok := r.BasicAuth()
			if !ok || user != "lacis" || password != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"Version":"3.1.2"}`))
		case "/api/rawdata":
			http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
		case "/elsewhere":
			followed = true
		}
	}))
	t.Cleanup(api.Close)
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")

	ctx, _ := newContext(t, nil, nil, "")
	ctx.Config.Collectors.Traefik = config.TraefikConfig{APIURL: api.URL, Username: "lacis", Password: "secret"}
	a, err := newTraefikAPI(ctx)
	require.NoError(t, err)
	assert.Nil(t, a.client.Transport.(*http.Transport).Proxy, "the proxy of the environment is not used")

	var version struct{ Version string }
	require.NoError(t, a.get(t.Context(), "/api/version", &version))
	assert.Equal(t, "3.1.2", version.Version)

	err = a.get(t.Context(), "/api/rawdata", &version)
	assert.ErrorContains(t, err, "307 Temporary Redirect")
	assert.False(t, followed, "the credentials must not follow a redirect")

	ctx.Config.Collectors.Traefik = config.TraefikConfig{APIURL: api.URL}
	a, err = newTraefikAPI(ctx)
	require.NoError(t, err)
	assert.ErrorContains(t, a.get(t.Context(), "/api/version", &version), "401")
}

func TestTraefikAPI_TLS(t *testing.T) {
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Version":"3.1.2"}`))
	}))
	t.Cleanup(api.Close)
	ctx, _ := newContext(t, nil, nil, "")
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: api.Certificate().Raw})
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/traefik-ca.pem", ca, 0o600))

	var version struct{ Version string }
	ctx.Config.Collectors.Traefik = config.TraefikConfig{APIURL: api.URL}
	a, err := newTraefikAPI(ctx)
	require.NoError(t, err)
	assert.ErrorContains(t, a.get(t.Context(), "/api/version", &version), "certificate")

	ctx.Config.Collectors.Traefik = config.TraefikConfig{APIURL: api.URL, CAFile: "/etc/lacis/traefik-ca.pem", CAOnly: true}
	a, err = newTraefikAPI(ctx)
	require.NoError(t, err)
	require.NoError(t, a.get(t.Context(), "/api/version", &version))
	assert.Equal(t, "3.1.2", version.Version)
}
