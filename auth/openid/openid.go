// Package openid runs the authorization code flow of an OpenID Connect
// provider, with PKCE and a nonce, and reads the claims Lacis keeps of the
// ID token.
package openid

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Timeout bounds every call to the provider.
const Timeout = 10 * time.Second

// ErrNonce refuses an ID token issued for another login.
var ErrNonce = errors.New("nonce of the ID token does not match")

// Claims are what Lacis keeps of an ID token.
type Claims struct {
	Subject   string
	Username  string
	FirstName string
	LastName  string
	Groups    []string
}

// Provider runs the code flow of the provider of the configuration.
type Provider interface {
	// AuthURL is the page of the provider the browser is sent to.
	AuthURL(ctx context.Context, state, verifier, nonce string) (string, error)
	// Exchange trades the code for an ID token, checks it and its nonce,
	// and returns its claims.
	Exchange(ctx context.Context, code, verifier, nonce string) (*Claims, error)
}

type provider struct {
	cfg         config.OIDCConfig
	redirectURL string
	client      *http.Client

	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// New returns the provider; redirectURL is the callback of Lacis. The
// provider is discovered at its first use, so that a provider down does
// not stop the server from starting.
func New(cfg config.OIDCConfig, redirectURL string) Provider {
	return &provider{cfg: cfg, redirectURL: redirectURL, client: &http.Client{Timeout: Timeout}}
}

// discover reads the configuration of the provider once it answers.
func (p *provider) discover() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.oauth != nil {
		return nil
	}
	// The context outlives the request: the provider keeps it to fetch its
	// signing keys later.
	ctx := oidc.ClientContext(context.Background(), p.client)
	op, err := oidc.NewProvider(ctx, p.cfg.Issuer)
	if err != nil {
		return fmt.Errorf("discover OIDC provider %s: %w", p.cfg.Issuer, err)
	}
	p.oauth = &oauth2.Config{ClientID: p.cfg.ClientID, ClientSecret: p.cfg.ClientSecret, Endpoint: op.Endpoint(),
		RedirectURL: p.redirectURL, Scopes: p.cfg.Scopes}
	p.verifier = op.Verifier(&oidc.Config{ClientID: p.cfg.ClientID})
	return nil
}

func (p *provider) AuthURL(_ context.Context, state, verifier, nonce string) (string, error) {
	if err := p.discover(); err != nil {
		return "", err
	}
	return p.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

func (p *provider) Exchange(ctx context.Context, code, verifier, nonce string) (*Claims, error) {
	if err := p.discover(); err != nil {
		return nil, err
	}
	ctx = oidc.ClientContext(ctx, p.client)
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("exchange the code: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, errors.New("no ID token in the token response")
	}
	idToken, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("verify the ID token: %w", err)
	}
	if idToken.Nonce != nonce {
		return nil, ErrNonce
	}
	var all map[string]any
	if err := idToken.Claims(&all); err != nil {
		return nil, fmt.Errorf("read the ID token: %w", err)
	}
	c := &Claims{Subject: idToken.Subject, Groups: groups(all[p.cfg.GroupsClaim])}
	c.Username, _ = all[p.cfg.UsernameClaim].(string)
	c.FirstName, _ = all["given_name"].(string)
	c.LastName, _ = all["family_name"].(string)
	return c, nil
}

// groups reads a groups claim: a list of names, or a single one.
func groups(v any) []string {
	switch g := v.(type) {
	case string:
		if g != "" {
			return []string{g}
		}
	case []any:
		out := make([]string, 0, len(g))
		for _, item := range g {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return []string{}
}
