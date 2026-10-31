package openid

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// fakeProvider is an OIDC provider issuing an ID token with the claims for
// the code "good", once the PKCE verifier matches the challenge.
type fakeProvider struct {
	srv       *httptest.Server
	key       *rsa.PrivateKey
	claims    map[string]any
	challenge string
}

func newFakeProvider(t *testing.T) *fakeProvider {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &fakeProvider{key: key}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/auth",
			"token_endpoint": f.srv.URL + "/token", "jwks_uri": f.srv.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if r.PostForm.Get("code") != "good" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 60, "id_token": f.sign(t)})
	})
	return f
}

func (f *fakeProvider) sign(t *testing.T) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
	require.NoError(t, err)
	claims := map[string]any{"iss": f.srv.URL, "aud": "lacis", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
	for k, v := range f.claims {
		claims[k] = v
	}
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	jws, err := signer.Sign(payload)
	require.NoError(t, err)
	raw, err := jws.CompactSerialize()
	require.NoError(t, err)
	return raw
}

func TestProvider(t *testing.T) {
	f := newFakeProvider(t)
	p := New(config.OIDCConfig{Issuer: f.srv.URL, ClientID: "lacis", ClientSecret: "s", Scopes: []string{"openid", "profile"}, UsernameClaim: "preferred_username",
		GroupsClaim: "roles"}, "https://lacis.example/auth/oidc/callback")
	ctx := context.Background()
	verifier := oauth2.GenerateVerifier()

	raw, err := p.AuthURL(ctx, "st", verifier, "n1")
	require.NoError(t, err)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	q := u.Query()
	assert.Equal(t, f.srv.URL+"/auth", u.Scheme+"://"+u.Host+u.Path)
	assert.Equal(t, "st", q.Get("state"))
	assert.Equal(t, "n1", q.Get("nonce"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Equal(t, "https://lacis.example/auth/oidc/callback", q.Get("redirect_uri"))
	assert.Equal(t, "openid profile", q.Get("scope"))
	f.challenge = q.Get("code_challenge")

	f.claims = map[string]any{"sub": "u-42", "preferred_username": "jane", "given_name": "Jane", "family_name": "Doe", "nonce": "n1",
		"roles": []string{"ops", "dev"}}
	c, err := p.Exchange(ctx, "good", verifier, "n1")
	require.NoError(t, err)
	assert.Equal(t, &Claims{Subject: "u-42", Username: "jane", FirstName: "Jane", LastName: "Doe", Groups: []string{"ops", "dev"}}, c)

	_, err = p.Exchange(ctx, "good", verifier, "n2")
	assert.ErrorIs(t, err, ErrNonce, "a token of another login")
	_, err = p.Exchange(ctx, "good", oauth2.GenerateVerifier(), "n1")
	assert.ErrorContains(t, err, "exchange the code", "PKCE: another verifier")
	_, err = p.Exchange(ctx, "bad", verifier, "n1")
	assert.ErrorContains(t, err, "invalid_grant")
}

func TestProvider_Down(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	p := New(config.OIDCConfig{Issuer: srv.URL, ClientID: "lacis"}, "https://lacis.example/auth/oidc/callback")
	_, err := p.AuthURL(context.Background(), "s", "v", "n")
	assert.ErrorContains(t, err, "discover OIDC provider")
	_, err = p.Exchange(context.Background(), "c", "v", "n")
	assert.ErrorContains(t, err, "discover OIDC provider")
}

func TestGroups(t *testing.T) {
	assert.Equal(t, []string{"ops"}, groups("ops"))
	assert.Equal(t, []string{"a", "b"}, groups([]any{"a", 3, "", "b"}))
	assert.Equal(t, []string{}, groups(nil))
	assert.Equal(t, []string{}, groups(""))
}
