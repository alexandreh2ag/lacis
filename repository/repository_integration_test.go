//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/alexandreh2ag/lacis/database/dbtest"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var ctx = context.Background()

// fixture holds a namespace, a network, an enrollment token, an agent and
// its host.
type fixture struct {
	repos     *Repositories
	db        *gorm.DB
	namespace *model.Namespace
	network   *model.Network
	token     *model.EnrollmentToken
	agent     *model.Agent
	host      *model.Host
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := dbtest.Open(t, "repository")
	f := &fixture{repos: NewRepositories(db), db: db}
	f.namespace = &model.Namespace{Slug: "acme", Name: "Acme"}
	require.NoError(t, f.repos.Namespace.Create(ctx, f.namespace))
	f.network = &model.Network{NamespaceID: f.namespace.ID, Slug: "default", Name: "Default"}
	require.NoError(t, f.repos.Network.Create(ctx, f.network))
	f.token = &model.EnrollmentToken{TokenHash: "enrollment-hash"}
	require.NoError(t, f.repos.EnrollmentToken.Create(ctx, f.token))
	f.agent = &model.Agent{TokenHash: "agent-hash", EnrollmentTokenID: f.token.ID}
	require.NoError(t, f.repos.Agent.Create(ctx, f.agent))
	now := time.Now().UTC().Truncate(time.Microsecond)
	f.host = &model.Host{
		AgentID:     f.agent.ID,
		NamespaceID: f.namespace.ID,
		Hostname:    "acme-site-prod-1",
		Labels:      labels.Labels{Namespace: "acme", Project: "site", Env: "prod"},
		FirstSeenAt: now,
		LastSeenAt:  now,
	}
	require.NoError(t, f.repos.Host.Create(ctx, f.host))
	return f
}

func TestNamespaceRepository(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.Namespace

	assert.Equal(t, uuid.Version(7), f.namespace.ID.Version())
	// Database defaults are read back on insert.
	assert.Equal(t, 7*24*time.Hour, f.namespace.StaleAfter.Duration())
	assert.Equal(t, 30*24*time.Hour, f.namespace.InactiveAfter.Duration())
	assert.False(t, f.namespace.CreatedAt.IsZero())

	other := &model.Namespace{Slug: "beta", Name: "Beta", StaleAfter: model.Interval(36 * time.Hour), InactiveAfter: model.Interval(72 * time.Hour)}
	require.NoError(t, repo.Create(ctx, other))

	got, err := repo.FindBySlug(ctx, "beta")
	require.NoError(t, err)
	assert.Equal(t, other.ID, got.ID)
	assert.Equal(t, 36*time.Hour, got.StaleAfter.Duration())
	assert.Equal(t, 72*time.Hour, got.InactiveAfter.Duration())

	got, err = repo.FindByID(ctx, f.namespace.ID)
	require.NoError(t, err)
	assert.Equal(t, "acme", got.Slug)

	all, err := repo.FindAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, []string{"acme", "beta"}, []string{all[0].Slug, all[1].Slug})

	_, err = repo.FindBySlug(ctx, "missing")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	err = repo.Create(ctx, &model.Namespace{Slug: "acme", Name: "Again"})
	assert.ErrorIs(t, err, gorm.ErrDuplicatedKey)
}

func TestNetworkRepository(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.Network

	assert.Equal(t, model.NetworkKindPrivate, f.network.Kind)

	got, err := repo.FindBySlug(ctx, f.namespace.ID, "default")
	require.NoError(t, err)
	assert.Equal(t, f.network.ID, got.ID)

	got, err = repo.FindByID(ctx, f.network.ID)
	require.NoError(t, err)
	assert.Equal(t, "Default", got.Name)

	_, err = repo.FindBySlug(ctx, uuid.New(), "default")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	err = repo.Create(ctx, &model.Network{NamespaceID: f.namespace.ID, Slug: "dmz", Name: "DMZ", Kind: "hybrid"})
	assert.ErrorIs(t, err, gorm.ErrCheckConstraintViolated)
}

func TestEnrollmentTokenRepository(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.EnrollmentToken
	label := "web servers"
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)

	token := &model.EnrollmentToken{TokenHash: "hash-1", Label: &label, ExpiresAt: &expires}
	require.NoError(t, repo.Create(ctx, token))
	require.NoError(t, repo.Create(ctx, &model.EnrollmentToken{TokenHash: "hash-2"}))

	got, err := repo.FindByHash(ctx, "hash-1")
	require.NoError(t, err)
	assert.Equal(t, token.ID, got.ID)
	assert.Equal(t, label, *got.Label)
	assert.True(t, expires.Equal(*got.ExpiresAt))

	tokens, err := repo.FindAll(ctx)
	require.NoError(t, err)
	require.Len(t, tokens, 3, "the fixture token, then both")
	assert.Equal(t, token.ID, tokens[1].ID)

	revokedAt := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Revoke(ctx, token.ID, revokedAt))
	require.NoError(t, repo.Revoke(ctx, token.ID, revokedAt.Add(time.Hour)))
	got, err = repo.FindByHash(ctx, "hash-1")
	require.NoError(t, err)
	require.NotNil(t, got.RevokedAt)
	assert.True(t, revokedAt.Equal(*got.RevokedAt), "a second revocation keeps the first date")

	assert.ErrorIs(t, repo.Revoke(ctx, uuid.New(), revokedAt), gorm.ErrRecordNotFound)
	assert.ErrorIs(t, repo.Create(ctx, &model.EnrollmentToken{TokenHash: "hash-2"}), gorm.ErrDuplicatedKey)
}

func TestAgentRepository(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.Agent

	assert.False(t, f.agent.CreatedAt.IsZero(), "created_at is read back on insert")

	got, err := repo.FindByTokenHash(ctx, "agent-hash")
	require.NoError(t, err)
	assert.Equal(t, f.agent.ID, got.ID)
	assert.Equal(t, f.token.ID, got.EnrollmentTokenID)
	assert.Nil(t, got.LastSeenAt)

	seenAt := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Touch(ctx, f.agent.ID, seenAt))
	got, err = repo.FindByTokenHash(ctx, "agent-hash")
	require.NoError(t, err)
	assert.True(t, seenAt.Equal(*got.LastSeenAt))

	got, err = repo.FindByID(ctx, f.agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "agent-hash", got.TokenHash)

	_, err = repo.FindByTokenHash(ctx, "missing")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	revokedAt := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.Revoke(ctx, f.agent.ID, revokedAt))
	require.NoError(t, repo.Revoke(ctx, f.agent.ID, revokedAt.Add(time.Hour)))
	got, err = repo.FindByID(ctx, f.agent.ID)
	require.NoError(t, err)
	require.NotNil(t, got.RevokedAt)
	assert.True(t, revokedAt.Equal(*got.RevokedAt), "a second revocation keeps the first date")
	assert.ErrorIs(t, repo.Revoke(ctx, uuid.New(), revokedAt), gorm.ErrRecordNotFound)

	assert.ErrorIs(t, repo.Create(ctx, &model.Agent{TokenHash: "agent-hash", EnrollmentTokenID: f.token.ID}), gorm.ErrDuplicatedKey)
	assert.ErrorIs(t, repo.Create(ctx, &model.Agent{TokenHash: "other", EnrollmentTokenID: uuid.New()}), gorm.ErrForeignKeyViolated)
	assert.ErrorIs(t, f.repos.Host.Create(ctx, &model.Host{AgentID: f.agent.ID, NamespaceID: f.namespace.ID, Hostname: "again"}), gorm.ErrDuplicatedKey, "one host per agent")
}

func TestHostRepository(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.Host

	got, err := repo.FindByAgentID(ctx, f.agent.ID)
	require.NoError(t, err)
	assert.Equal(t, f.host.ID, got.ID)
	assert.Equal(t, f.host.Labels, got.Labels)
	assert.Nil(t, got.OS)
	assert.True(t, f.host.FirstSeenAt.Equal(got.FirstSeenAt))

	got.OS = json.RawMessage(`{"id":"debian","version":"12"}`)
	hash := "snapshot-hash"
	got.LastSnapshotHash = &hash
	got.LastSeenAt = got.LastSeenAt.Add(time.Minute)
	require.NoError(t, repo.Update(ctx, got))

	got, err = repo.FindByID(ctx, f.host.ID)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"debian","version":"12"}`, string(got.OS))
	assert.Equal(t, hash, *got.LastSnapshotHash)
	assert.True(t, f.host.LastSeenAt.Add(time.Minute).Equal(got.LastSeenAt))

	_, err = repo.FindByID(ctx, uuid.New())
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// A nil raw JSON is stored as SQL NULL, not as the JSON null.
	var isNull bool
	require.NoError(t, f.db.Raw("select resources is null from hosts where id = ?", f.host.ID).Scan(&isNull).Error)
	assert.True(t, isNull)
}

func TestSnapshotRepository(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.Snapshot
	base := time.Now().UTC().Truncate(time.Microsecond)

	older := &model.Snapshot{HostID: f.host.ID, CollectedAt: base, Hash: "h1", SchemaVersion: 1, Payload: json.RawMessage(`{"schema_version":1}`)}
	newer := &model.Snapshot{HostID: f.host.ID, CollectedAt: base.Add(time.Hour), Hash: "h2", SchemaVersion: 1, Payload: json.RawMessage(`{"schema_version":1,"n":2}`)}
	require.NoError(t, repo.Create(ctx, newer))
	require.NoError(t, repo.Create(ctx, older))
	assert.NotZero(t, older.ID)
	assert.False(t, older.ReceivedAt.IsZero())

	got, err := repo.FindLatestByHost(ctx, f.host.ID)
	require.NoError(t, err)
	assert.Equal(t, newer.ID, got.ID)
	assert.JSONEq(t, `{"schema_version":1,"n":2}`, string(got.Payload))

	got, err = repo.FindByID(ctx, older.ID)
	require.NoError(t, err)
	assert.Equal(t, "h1", got.Hash)

	_, err = repo.FindLatestByHost(ctx, uuid.New())
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = repo.FindByID(ctx, -1)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestUserRepository(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.User
	hash := "argon2id-hash"

	user := &model.User{Username: "admin", PasswordHash: &hash}
	require.NoError(t, repo.Create(ctx, user))
	assert.False(t, user.CreatedAt.IsZero())

	got, err := repo.FindByUsername(ctx, "admin")
	require.NoError(t, err)
	assert.Equal(t, user.ID, got.ID)
	assert.Equal(t, hash, *got.PasswordHash)

	name := "Admin"
	got.FirstName = &name
	require.NoError(t, repo.Update(ctx, got))
	got, err = repo.FindByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, name, *got.FirstName)

	_, err = repo.FindByUsername(ctx, "nobody")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	assert.ErrorIs(t, repo.Create(ctx, &model.User{Username: "admin"}), gorm.ErrDuplicatedKey)
}

func TestWithTx(t *testing.T) {
	f := newFixture(t)
	errRollback := errors.New("rollback")

	err := f.db.Transaction(func(tx *gorm.DB) error {
		repos := []func() error{
			func() error {
				return f.repos.Namespace.WithTx(tx).Create(ctx, &model.Namespace{Slug: "tx", Name: "Tx"})
			},
			func() error {
				return f.repos.Network.WithTx(tx).Create(ctx, &model.Network{NamespaceID: f.namespace.ID, Slug: "tx", Name: "Tx"})
			},
			func() error {
				return f.repos.EnrollmentToken.WithTx(tx).Create(ctx, &model.EnrollmentToken{TokenHash: "tx"})
			},
			func() error {
				agent := &model.Agent{TokenHash: "tx", EnrollmentTokenID: f.token.ID}
				if err := f.repos.Agent.WithTx(tx).Create(ctx, agent); err != nil {
					return err
				}
				h := *f.host
				h.ID, h.AgentID = uuid.Nil, agent.ID
				return f.repos.Host.WithTx(tx).Create(ctx, &h)
			},
			func() error {
				return f.repos.Snapshot.WithTx(tx).Create(ctx, &model.Snapshot{HostID: f.host.ID, CollectedAt: time.Now(), Hash: "tx", SchemaVersion: 1, Payload: json.RawMessage(`{}`)})
			},
			func() error { return f.repos.User.WithTx(tx).Create(ctx, &model.User{Username: "tx"}) },
		}
		for _, create := range repos {
			if err := create(); err != nil {
				return err
			}
		}
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)

	for table, want := range map[string]int64{"namespaces": 1, "networks": 1, "enrollment_tokens": 1, "agents": 1, "hosts": 1, "snapshots": 0, "users": 0} {
		var count int64
		require.NoError(t, f.db.Table(table).Count(&count).Error)
		assert.Equal(t, want, count, table)
	}
}

func TestNetworkTypes(t *testing.T) {
	f := newFixture(t)

	cidr := model.NetworkCIDR{NetworkID: f.network.ID, NamespaceID: f.namespace.ID, CIDR: model.CIDR{Prefix: netip.MustParsePrefix("10.0.0.0/24")}}
	require.NoError(t, f.db.Create(&cidr).Error)
	iface := "eth0"
	ips := []model.HostIP{
		{HostID: f.host.ID, Interface: &iface, IP: model.IP{Addr: netip.MustParseAddr("10.0.0.12")}},
		{HostID: f.host.ID, IP: model.IP{Addr: netip.MustParseAddr("fd00::12")}},
	}
	require.NoError(t, f.db.Create(&ips).Error)

	var gotCIDR model.NetworkCIDR
	require.NoError(t, f.db.First(&gotCIDR).Error)
	assert.Equal(t, cidr.CIDR, gotCIDR.CIDR)

	var gotIPs []model.HostIP
	require.NoError(t, f.db.Order("ip").Find(&gotIPs).Error)
	require.Len(t, gotIPs, 2)
	assert.Equal(t, ips[0].IP, gotIPs[0].IP)
	assert.Equal(t, iface, *gotIPs[0].Interface)
	assert.Equal(t, ips[1].IP, gotIPs[1].IP)

	// The gist index answers "which network contains this IP".
	var networkID uuid.UUID
	require.NoError(t, f.db.Raw("select network_id from network_cidrs where cidr >>= ?::inet", "10.0.0.12").Row().Scan(&networkID))
	assert.Equal(t, f.network.ID, networkID)
}

func TestRoleRepository(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.Role

	admin, err := repo.FindByCode(ctx, model.RoleAdmin)
	require.NoError(t, err, "the admin role is seeded")

	viewer := &model.Role{Code: "acme-viewer"}
	require.NoError(t, repo.Create(ctx, viewer))
	assert.False(t, viewer.CreatedAt.IsZero())

	all, err := repo.FindAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, []string{"acme-viewer", "admin"}, []string{all[0].Code, all[1].Code})

	user := &model.User{Username: "bob"}
	require.NoError(t, f.repos.User.Create(ctx, user))
	require.NoError(t, repo.AssignToUser(ctx, user.ID, admin.ID))
	require.NoError(t, repo.AssignToUser(ctx, user.ID, viewer.ID))
	require.NoError(t, repo.AssignToUser(ctx, user.ID, viewer.ID), "assigning twice is a no-op")

	roles, err := repo.FindByUser(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, roles, 2)
	assert.Equal(t, []string{"acme-viewer", "admin"}, []string{roles[0].Code, roles[1].Code})

	_, err = repo.FindByCode(ctx, "missing")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	assert.ErrorIs(t, repo.Create(ctx, &model.Role{Code: "admin"}), gorm.ErrDuplicatedKey)
	assert.ErrorIs(t, repo.AssignToUser(ctx, uuid.New(), admin.ID), gorm.ErrForeignKeyViolated)

	require.NoError(t, f.db.Transaction(func(tx *gorm.DB) error {
		return repo.WithTx(tx).Create(ctx, &model.Role{Code: "in-tx"})
	}))
	_, err = repo.FindByCode(ctx, "in-tx")
	assert.NoError(t, err)
}

func TestTransactor(t *testing.T) {
	f := newFixture(t)
	tr := NewTransactor(f.db)

	require.NoError(t, tr.InTx(ctx, func(repos *Repositories) error {
		return repos.User.Create(ctx, &model.User{Username: "committed"})
	}))
	_, err := f.repos.User.FindByUsername(ctx, "committed")
	assert.NoError(t, err)

	errRollback := errors.New("rollback")
	err = tr.InTx(ctx, func(repos *Repositories) error {
		if err := repos.User.Create(ctx, &model.User{Username: "rolled-back"}); err != nil {
			return err
		}
		return errRollback
	})
	assert.ErrorIs(t, err, errRollback)
	_, err = f.repos.User.FindByUsername(ctx, "rolled-back")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
