package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/auth/openid"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/hash"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"gorm.io/gorm"
)

var (
	// ErrTooManyAttempts refuses a login after too many failures from the
	// same IP or for the same email.
	ErrTooManyAttempts = errors.New("too many login attempts")
	// ErrLocalAuthDisabled refuses a local login when auth.local is off.
	ErrLocalAuthDisabled = errors.New("local authentication is disabled")
	// ErrOIDCDisabled refuses an OIDC login when auth.oidc is off.
	ErrOIDCDisabled = errors.New("OIDC authentication is disabled")
	// ErrOIDCAccount refuses an OIDC identity no user can take: without a
	// valid username, or whose username is another account's.
	ErrOIDCAccount = errors.New("no account for this OIDC identity")
)

const (
	// LoginWindow is the period over which the failed logins are counted.
	LoginWindow = 15 * time.Minute
	// MaxFailuresPerUsername and MaxFailuresPerIP are the failed logins
	// allowed in LoginWindow.
	MaxFailuresPerUsername = 5
	MaxFailuresPerIP       = 20
	// touchEvery limits the writes of the last access of a session.
	touchEvery = time.Minute
)

type LoginInput struct {
	Username string
	Password string
	// IP is the address of the client, for the limit of attempts.
	IP string
}

// Login is an opened session: the token goes in the cookie.
type Login struct {
	Token     string
	ExpiresAt time.Time
}

type AuthService interface {
	// Login checks a local password and opens a session. It returns
	// ErrUnauthorized for an unknown username, a wrong password or a user
	// without password, ErrTooManyAttempts after too many failures.
	Login(ctx context.Context, in LoginInput) (*Login, error)
	// LoginOIDC opens a session for an identity the OIDC provider checked:
	// the user of its subject, else a new user without role, unless its
	// username is another account's. The user takes the username and names
	// of the provider; the groups of the identity give its roles for the
	// session.
	LoginOIDC(ctx context.Context, claims openid.Claims) (*Login, error)
	// Authenticate returns the principal of a session token, and pushes
	// back the expiration of the session; ErrUnauthorized when the token
	// is unknown or expired.
	Authenticate(ctx context.Context, token string) (*auth.Principal, error)
	// Logout closes the session of the token; an unknown token is no
	// error.
	Logout(ctx context.Context, token string) error
}

type authService struct {
	appCtx   *appContext.Context
	tx       repository.Transactor
	failures *failures
}

func NewAuthService(appCtx *appContext.Context, tx repository.Transactor) AuthService {
	return &authService{appCtx: appCtx, tx: tx, failures: newFailures()}
}

func (s *authService) Login(ctx context.Context, in LoginInput) (*Login, error) {
	if !s.appCtx.Config.Auth.Local.Enabled {
		return nil, ErrLocalAuthDisabled
	}
	username := strings.ToLower(strings.TrimSpace(in.Username))
	now := s.appCtx.Clock.Now()
	keys := []string{"username:" + username, "ip:" + in.IP}
	if s.failures.count(keys[0], now) >= MaxFailuresPerUsername || s.failures.count(keys[1], now) >= MaxFailuresPerIP {
		return nil, ErrTooManyAttempts
	}
	var login *Login
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		user, err := repos.User.FindByUsername(ctx, username)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Hash anyway, so that an unknown username takes as long as a
			// wrong password.
			_, _ = hash.Password(in.Password)
			return ErrUnauthorized
		}
		if err != nil {
			return err
		}
		if user.PasswordHash == nil {
			return ErrUnauthorized
		}
		ok, err := hash.VerifyPassword(in.Password, *user.PasswordHash)
		if err != nil {
			return err
		}
		if !ok {
			return ErrUnauthorized
		}
		if _, err := repos.Session.DeleteExpired(ctx, now); err != nil {
			return err
		}
		login, err = s.open(ctx, repos, user, nil, now)
		return err
	})
	if errors.Is(err, ErrUnauthorized) {
		s.failures.add(keys, now)
	}
	if err != nil {
		return nil, err
	}
	s.failures.reset(keys[0])
	return login, nil
}

// open creates a session of the user; groups are its OIDC groups.
func (s *authService) open(ctx context.Context, repos *repository.Repositories, user *model.User, groups []string, now time.Time) (*Login, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	expires := now.Add(s.appCtx.Config.Auth.Session.TTL)
	session := &model.Session{ID: sessionID(token), UserID: user.ID, OIDCGroups: model.StringArray(groups),
		CreatedAt: now, LastSeenAt: now, ExpiresAt: expires}
	if session.OIDCGroups == nil {
		session.OIDCGroups = model.StringArray{}
	}
	if err := repos.Session.Create(ctx, session); err != nil {
		return nil, err
	}
	return &Login{Token: token, ExpiresAt: expires}, nil
}

func (s *authService) LoginOIDC(ctx context.Context, c openid.Claims) (*Login, error) {
	if !s.appCtx.Config.Auth.OIDC.Enabled {
		return nil, ErrOIDCDisabled
	}
	username, err := Username(c.Username)
	if c.Subject == "" || err != nil {
		return nil, ErrOIDCAccount
	}
	now := s.appCtx.Clock.Now()
	var login *Login
	err = s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		user, err := s.oidcUser(ctx, repos, c, username, now)
		if err != nil {
			return err
		}
		if _, err := repos.Session.DeleteExpired(ctx, now); err != nil {
			return err
		}
		login, err = s.open(ctx, repos, user, c.Groups, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return login, nil
}

// oidcUser finds or creates the user of an OIDC identity, and updates its
// username and names.
func (s *authService) oidcUser(ctx context.Context, repos *repository.Repositories, c openid.Claims, username string, now time.Time) (*model.User, error) {
	firstName, lastName := optional(strings.TrimSpace(c.FirstName)), optional(strings.TrimSpace(c.LastName))
	user, err := repos.User.FindByOIDCSubject(ctx, c.Subject)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		_, err := repos.User.FindByUsername(ctx, username)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			user = &model.User{Username: username, FirstName: firstName, LastName: lastName, OIDCSubject: &c.Subject, CreatedAt: now}
			return user, repos.User.Create(ctx, user)
		case err != nil:
			return nil, err
		}
		// No provider vouches for a username: taking the account of the
		// same username would let anyone in.
		return nil, ErrOIDCAccount
	} else if err != nil {
		return nil, err
	}
	if user.Username != username {
		// The provider changed the username: follow it, unless another
		// account has it.
		_, err := repos.User.FindByUsername(ctx, username)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			user.Username = username
		case err != nil:
			return nil, err
		}
	}
	if firstName != nil {
		user.FirstName = firstName
	}
	if lastName != nil {
		user.LastName = lastName
	}
	return user, repos.User.Update(ctx, user)
}

func (s *authService) Authenticate(ctx context.Context, token string) (*auth.Principal, error) {
	if token == "" {
		return nil, ErrUnauthorized
	}
	now := s.appCtx.Clock.Now()
	var principal *auth.Principal
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		session, err := repos.Session.FindValid(ctx, sessionID(token), now)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUnauthorized
		}
		if err != nil {
			return err
		}
		if now.Sub(session.LastSeenAt) >= touchEvery {
			if err := repos.Session.Touch(ctx, session.ID, now, now.Add(s.appCtx.Config.Auth.Session.TTL)); err != nil {
				return err
			}
		}
		user, err := repos.User.FindByID(ctx, session.UserID)
		if err != nil {
			return err
		}
		ns, global, err := repos.Role.FindPermissions(ctx, user.ID, session.OIDCGroups)
		if err != nil {
			return err
		}
		// The groups of auth.oidc.admin_groups hold every permission, as
		// long as the configuration names them.
		for _, g := range session.OIDCGroups {
			if slices.Contains(s.appCtx.Config.Auth.OIDC.AdminGroups, g) {
				ns = append(ns, model.NamespacePermission{Resource: model.ResourceAll, Action: model.ActionAll})
				global = append(global, model.GlobalPermission{Resource: model.ResourceAll, Action: model.ActionAll})
				break
			}
		}
		principal = &auth.Principal{User: *user, SessionID: session.ID, Namespace: ns, Global: global}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return principal, nil
}

func (s *authService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		return repos.Session.Delete(ctx, sessionID(token))
	})
}

// sessionID is what the sessions table holds of a token.
func sessionID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// failures counts the failed logins per key over LoginWindow, in memory:
// a restart forgets them, several instances count apart.
type failures struct {
	mu   sync.Mutex
	seen map[string][]time.Time
}

func newFailures() *failures {
	return &failures{seen: map[string][]time.Time{}}
}

func (f *failures) count(key string, now time.Time) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.seen[key][:0]
	for _, at := range f.seen[key] {
		if now.Sub(at) < LoginWindow {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(f.seen, key)
		return 0
	}
	f.seen[key] = kept
	return len(kept)
}

func (f *failures) add(keys []string, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		f.seen[k] = append(f.seen[k], now)
	}
}

func (f *failures) reset(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.seen, key)
}
