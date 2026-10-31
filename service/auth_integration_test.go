//go:build integration

package service

import (
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/auth/openid"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database/dbtest"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// fakeClock is a clock the test moves.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time                         { return c.now }
func (c *fakeClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type authFixture struct {
	db    *gorm.DB
	repos *repository.Repositories
	clock *fakeClock
	svc   *authService
	ns    *model.Namespace
}

func newAuthFixture(t *testing.T) *authFixture {
	db := dbtest.Open(t, "normalize")
	appCtx := appContext.TestContext(nil)
	clock := &fakeClock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	appCtx.Clock = clock
	appCtx.Config.Auth.Session.TTL = time.Hour
	appCtx.Config.Auth.OIDC.Enabled = true
	appCtx.Config.Auth.OIDC.AdminGroups = []string{"lacis-admins"}
	tx := repository.NewTransactor(db)
	f := &authFixture{db: db, repos: repository.NewRepositories(db), clock: clock, svc: NewAuthService(appCtx, tx).(*authService)}
	_, err := NewUserService(appCtx, tx).Create(ctx, CreateUserInput{Username: "admin", Password: "correct horse", Roles: []string{model.RoleAdmin}})
	require.NoError(t, err)
	_, err = NewUserService(appCtx, tx).Create(ctx, CreateUserInput{Username: "bob", Password: "battery staple"})
	require.NoError(t, err)
	f.ns = &model.Namespace{Slug: "acme", Name: "Acme"}
	require.NoError(t, f.repos.Namespace.Create(ctx, f.ns))
	return f
}

func TestAuth_LoginAndAuthenticate(t *testing.T) {
	f := newAuthFixture(t)
	login, err := f.svc.Login(ctx, LoginInput{Username: " Admin ", Password: "correct horse", IP: "10.0.0.1"})
	require.NoError(t, err)
	assert.Equal(t, f.clock.now.Add(time.Hour), login.ExpiresAt)
	assert.Len(t, login.Token, 43)

	p, err := f.svc.Authenticate(ctx, login.Token)
	require.NoError(t, err)
	assert.Equal(t, "admin", p.User.Username)
	assert.True(t, p.Can(f.ns.ID, model.ResourceInventory, model.ActionRead), "admin")
	assert.True(t, p.CanGlobal(model.ResourceUsers, model.ActionWrite))
	var stored model.Session
	require.NoError(t, f.db.First(&stored).Error)
	assert.NotEqual(t, login.Token, stored.ID, "the table holds a hash of the token")

	// Each access, past a minute, pushes back the expiration.
	f.clock.now = f.clock.now.Add(50 * time.Minute)
	_, err = f.svc.Authenticate(ctx, login.Token)
	require.NoError(t, err)
	f.clock.now = f.clock.now.Add(50 * time.Minute)
	_, err = f.svc.Authenticate(ctx, login.Token)
	require.NoError(t, err, "still valid: touched 50 minutes ago")
	f.clock.now = f.clock.now.Add(61 * time.Minute)
	_, err = f.svc.Authenticate(ctx, login.Token)
	assert.ErrorIs(t, err, ErrUnauthorized, "expired")

	// The next login purges the expired sessions.
	_, err = f.svc.Login(ctx, LoginInput{Username: "bob", Password: "battery staple", IP: "10.0.0.1"})
	require.NoError(t, err)
	var count int64
	require.NoError(t, f.db.Model(&model.Session{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	_, err = f.svc.Authenticate(ctx, "")
	assert.ErrorIs(t, err, ErrUnauthorized)
	_, err = f.svc.Authenticate(ctx, "unknown")
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestAuth_LoginRefused(t *testing.T) {
	f := newAuthFixture(t)
	oidc := &model.User{Username: "oidc"}
	require.NoError(t, f.repos.User.Create(ctx, oidc))
	for _, in := range []LoginInput{
		{Username: "admin", Password: "wrong"},
		{Username: "nobody", Password: "correct horse"},
		{Username: "oidc", Password: ""},
	} {
		_, err := f.svc.Login(ctx, in)
		assert.ErrorIs(t, err, ErrUnauthorized, in.Username)
	}

	f.svc.appCtx.Config.Auth.Local.Enabled = false
	_, err := f.svc.Login(ctx, LoginInput{Username: "admin", Password: "correct horse"})
	assert.ErrorIs(t, err, ErrLocalAuthDisabled)
}

func TestAuth_TooManyAttempts(t *testing.T) {
	f := newAuthFixture(t)
	for range MaxFailuresPerUsername {
		_, err := f.svc.Login(ctx, LoginInput{Username: "admin", Password: "wrong", IP: "10.0.0.1"})
		require.ErrorIs(t, err, ErrUnauthorized)
	}
	_, err := f.svc.Login(ctx, LoginInput{Username: "admin", Password: "correct horse", IP: "10.0.0.2"})
	assert.ErrorIs(t, err, ErrTooManyAttempts, "per username, whatever the IP")
	_, err = f.svc.Login(ctx, LoginInput{Username: "bob", Password: "battery staple", IP: "10.0.0.1"})
	assert.NoError(t, err, "another username from the same IP")

	for i := range MaxFailuresPerIP {
		_, err := f.svc.Login(ctx, LoginInput{Username: uuid.NewString(), Password: "wrong", IP: "10.0.0.9"})
		require.ErrorIs(t, err, ErrUnauthorized, i)
	}
	_, err = f.svc.Login(ctx, LoginInput{Username: "bob", Password: "battery staple", IP: "10.0.0.9"})
	assert.ErrorIs(t, err, ErrTooManyAttempts, "per IP")

	f.clock.now = f.clock.now.Add(LoginWindow)
	_, err = f.svc.Login(ctx, LoginInput{Username: "admin", Password: "correct horse", IP: "10.0.0.9"})
	assert.NoError(t, err, "the failures are forgotten after the window")
}

func TestAuth_OIDCGroupsAndLogout(t *testing.T) {
	f := newAuthFixture(t)
	other := &model.Namespace{Slug: "other", Name: "Other"}
	require.NoError(t, f.repos.Namespace.Create(ctx, other))
	viewer := &model.Role{Code: "acme-viewer"}
	require.NoError(t, f.repos.Role.Create(ctx, viewer))
	require.NoError(t, f.db.Create(&model.RoleOIDCGroup{RoleID: viewer.ID, GroupName: "devs"}).Error)
	require.NoError(t, f.db.Create(&model.NamespacePermission{RoleID: viewer.ID, NamespaceID: &f.ns.ID,
		Resource: model.ResourceInventory, Action: model.ActionRead}).Error)
	bob, err := f.repos.User.FindByUsername(ctx, "bob")
	require.NoError(t, err)

	login, err := f.svc.open(ctx, f.repos, bob, []string{"devs", "unknown"}, f.clock.now)
	require.NoError(t, err)
	p, err := f.svc.Authenticate(ctx, login.Token)
	require.NoError(t, err)
	assert.True(t, p.Can(f.ns.ID, model.ResourceInventory, model.ActionRead))
	assert.False(t, p.Can(f.ns.ID, model.ResourceInventory, model.ActionWrite))
	assert.False(t, p.Can(other.ID, model.ResourceInventory, model.ActionRead))
	assert.False(t, p.CanGlobal(model.ResourceUsers, model.ActionRead))
	assert.Equal(t, []uuid.UUID{f.ns.ID}, p.NamespaceIDs(model.ResourceInventory, model.ActionRead))

	plain, err := f.svc.Login(ctx, LoginInput{Username: "bob", Password: "battery staple"})
	require.NoError(t, err)
	p, err = f.svc.Authenticate(ctx, plain.Token)
	require.NoError(t, err)
	assert.Empty(t, p.Namespace, "without the group, no role")

	require.NoError(t, f.svc.Logout(ctx, login.Token))
	_, err = f.svc.Authenticate(ctx, login.Token)
	assert.ErrorIs(t, err, ErrUnauthorized)
	require.NoError(t, f.svc.Logout(ctx, ""))
	require.NoError(t, f.svc.Logout(ctx, "unknown"))
}

func TestAuth_LoginOIDC(t *testing.T) {
	f := newAuthFixture(t)
	jane := openid.Claims{Subject: "u-1", Username: "Jane", FirstName: "Jane", Groups: []string{"devs"}}

	// A new identity makes a user without role.
	login, err := f.svc.LoginOIDC(ctx, jane)
	require.NoError(t, err)
	p, err := f.svc.Authenticate(ctx, login.Token)
	require.NoError(t, err)
	assert.Equal(t, "jane", p.User.Username)
	assert.Equal(t, "Jane", *p.User.FirstName)
	assert.Nil(t, p.User.LastName)
	assert.Equal(t, "u-1", *p.User.OIDCSubject)
	assert.Nil(t, p.User.PasswordHash)
	assert.Empty(t, p.Namespace)
	first := p.User.ID

	// The same subject finds the user and follows the provider; a name the
	// provider does not give is kept.
	jane.Username, jane.FirstName, jane.LastName = "jane.doe", "", "Doe"
	login, err = f.svc.LoginOIDC(ctx, jane)
	require.NoError(t, err)
	p, err = f.svc.Authenticate(ctx, login.Token)
	require.NoError(t, err)
	assert.Equal(t, first, p.User.ID)
	assert.Equal(t, "jane.doe", p.User.Username)
	assert.Equal(t, "Jane Doe", p.User.FullName())

	// A local account is never taken by its username: no provider vouches
	// for it.
	_, err = f.svc.LoginOIDC(ctx, openid.Claims{Subject: "u-2", Username: "bob"})
	assert.ErrorIs(t, err, ErrOIDCAccount)
	_, err = f.svc.LoginOIDC(ctx, openid.Claims{Subject: "u-3", Username: "jane.doe"})
	assert.ErrorIs(t, err, ErrOIDCAccount, "nor an account tied to another identity")
	_, err = f.svc.LoginOIDC(ctx, openid.Claims{Subject: "u-4"})
	assert.ErrorIs(t, err, ErrOIDCAccount, "no username")
	_, err = f.svc.LoginOIDC(ctx, openid.Claims{Subject: "u-5", Username: "Jane Doe"})
	assert.ErrorIs(t, err, ErrOIDCAccount, "not a valid username")

	// A username change to another account's is not followed.
	jane.Username = "admin"
	login, err = f.svc.LoginOIDC(ctx, jane)
	require.NoError(t, err)
	p, err = f.svc.Authenticate(ctx, login.Token)
	require.NoError(t, err)
	assert.Equal(t, "jane.doe", p.User.Username)

	// The admin groups of the configuration hold every permission.
	jane.Groups = []string{"devs", "lacis-admins"}
	login, err = f.svc.LoginOIDC(ctx, jane)
	require.NoError(t, err)
	p, err = f.svc.Authenticate(ctx, login.Token)
	require.NoError(t, err)
	assert.True(t, p.Can(f.ns.ID, model.ResourceMetadata, model.ActionWrite))
	assert.True(t, p.CanGlobal(model.ResourceUsers, model.ActionWrite))
	f.svc.appCtx.Config.Auth.OIDC.AdminGroups = nil
	p, err = f.svc.Authenticate(ctx, login.Token)
	require.NoError(t, err)
	assert.False(t, p.CanGlobal(model.ResourceUsers, model.ActionWrite), "a group left out of the configuration gives nothing")

	f.svc.appCtx.Config.Auth.OIDC.Enabled = false
	_, err = f.svc.LoginOIDC(ctx, jane)
	assert.ErrorIs(t, err, ErrOIDCDisabled)
}
