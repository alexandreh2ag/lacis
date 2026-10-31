package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/hash"
	mockQueue "github.com/alexandreh2ag/lacis/mocks/queue"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"
)

var raw = json.RawMessage(`{"raw":true}`)

func newSnapshot(namespace string) *snapshot.Snapshot {
	s := snapshot.New(snapshot.AgentInfo{Version: "1.2.0", OS: "linux", Arch: "amd64"},
		labels.Labels{Namespace: namespace, Project: "site", Env: "prod"}, "acme-site-prod-1")
	s.CollectedAt = now.Add(-time.Second)
	return s
}

func snapshotHash(t *testing.T, s *snapshot.Snapshot) string {
	h, err := s.Hash()
	require.NoError(t, err)
	return h
}

func TestSnapshotService_Authenticate(t *testing.T) {
	errDB := errors.New("db down")
	agent := &model.Agent{Base: model.Base{ID: uuid.New()}}
	tests := []struct {
		name    string
		agent   *model.Agent
		findErr error
		wantErr error
	}{
		{name: "valid", agent: agent},
		{name: "unknown", findErr: gorm.ErrRecordNotFound, wantErr: ErrUnauthorized},
		{name: "revoked", agent: &model.Agent{RevokedAt: &now}, wantErr: ErrUnauthorized},
		{name: "lookup fails", findErr: errDB, wantErr: errDB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			m.agent.EXPECT().FindByTokenHash(ctx, hash.Token("lacis_at_secret")).Return(tt.agent, tt.findErr)
			got, err := NewSnapshotService(testContext(t), m.tx, m.agent, nil).Authenticate(ctx, "lacis_at_secret")
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, agent, got)
		})
	}
}

func TestSnapshotService_Ingest_FirstSnapshot(t *testing.T) {
	for _, nsExists := range []bool{true, false} {
		t.Run(map[bool]string{true: "existing namespace", false: "new namespace"}[nsExists], func(t *testing.T) {
			m := newMocks(t)
			q := mockQueue.NewMockQueue(gomock.NewController(t))
			appCtx := testContext(t)
			agent := &model.Agent{Base: model.Base{ID: uuid.New()}}
			namespace := &model.Namespace{Base: model.Base{ID: uuid.New()}, Slug: "acme"}
			s := newSnapshot("acme")

			m.host.EXPECT().FindByAgentID(ctx, agent.ID).Return(nil, gorm.ErrRecordNotFound)
			if nsExists {
				m.namespace.EXPECT().FindBySlug(ctx, "acme").Return(namespace, nil)
			} else {
				m.namespace.EXPECT().FindBySlug(ctx, "acme").Return(nil, gorm.ErrRecordNotFound)
				m.namespace.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ any, ns *model.Namespace) error {
					assert.Equal(t, "acme", ns.Slug)
					assert.Equal(t, appCtx.Config.Defaults.StaleAfter, ns.StaleAfter.Duration())
					ns.ID = namespace.ID
					return nil
				})
			}
			hostID := uuid.New()
			m.host.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ any, h *model.Host) error {
				assert.Equal(t, agent.ID, h.AgentID)
				assert.Equal(t, namespace.ID, h.NamespaceID)
				assert.Equal(t, now, h.FirstSeenAt)
				h.ID = hostID
				return nil
			})
			m.agent.EXPECT().Touch(ctx, agent.ID, now).Return(nil)
			m.host.EXPECT().Update(ctx, gomock.Any()).DoAndReturn(func(_ any, h *model.Host) error {
				assert.Equal(t, "acme-site-prod-1", h.Hostname)
				assert.Equal(t, s.Host.Labels, h.Labels)
				assert.Equal(t, "1.2.0", *h.AgentVersion)
				assert.Equal(t, snapshotHash(t, s), *h.LastSnapshotHash)
				assert.Equal(t, now, h.LastSeenAt)
				return nil
			})
			m.snapshot.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ any, stored *model.Snapshot) error {
				assert.Equal(t, hostID, stored.HostID)
				assert.Equal(t, s.CollectedAt, stored.CollectedAt)
				assert.Equal(t, snapshotHash(t, s), stored.Hash)
				assert.Equal(t, 1, stored.SchemaVersion)
				assert.Equal(t, raw, stored.Payload)
				stored.ID = 42
				return nil
			})
			q.EXPECT().InsertNormalizeSnapshot(ctx, gomock.Any(), int64(42)).Return(nil)

			got, err := NewSnapshotService(appCtx, m.tx, m.agent, q).Ingest(ctx, agent, s, raw)
			require.NoError(t, err)
			assert.Equal(t, Accepted, got)
		})
	}
}

func TestSnapshotService_Ingest_KnownHost(t *testing.T) {
	agent := &model.Agent{Base: model.Base{ID: uuid.New()}}
	namespace := &model.Namespace{Base: model.Base{ID: uuid.New()}, Slug: "acme"}
	s := newSnapshot("acme")
	tests := []struct {
		name     string
		lastHash string
		want     IngestResult
	}{
		{name: "changed", lastHash: "sha256:previous", want: Accepted},
		{name: "unchanged", lastHash: snapshotHash(t, s), want: Unchanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			q := mockQueue.NewMockQueue(gomock.NewController(t))
			host := &model.Host{Base: model.Base{ID: uuid.New()}, NamespaceID: namespace.ID, LastSnapshotHash: &tt.lastHash}
			m.host.EXPECT().FindByAgentID(ctx, agent.ID).Return(host, nil)
			m.namespace.EXPECT().FindByID(ctx, namespace.ID).Return(namespace, nil)
			m.agent.EXPECT().Touch(ctx, agent.ID, now).Return(nil)
			m.host.EXPECT().Update(ctx, host).Return(nil)
			if tt.want == Accepted {
				m.snapshot.EXPECT().Create(ctx, gomock.Any()).Return(nil)
				q.EXPECT().InsertNormalizeSnapshot(ctx, gomock.Any(), gomock.Any()).Return(nil)
			}

			got, err := NewSnapshotService(testContext(t), m.tx, m.agent, q).Ingest(ctx, agent, s, raw)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, now, host.LastSeenAt)
		})
	}
}

func TestSnapshotService_Ingest_Errors(t *testing.T) {
	errDB := errors.New("db down")
	agent := &model.Agent{Base: model.Base{ID: uuid.New()}}
	namespace := &model.Namespace{Base: model.Base{ID: uuid.New()}, Slug: "acme"}
	knownHost := func() *model.Host { return &model.Host{Base: model.Base{ID: uuid.New()}, NamespaceID: namespace.ID} }
	tests := []struct {
		name    string
		snap    func() *snapshot.Snapshot
		setup   func(m *mocks, q *mockQueue.MockQueue)
		wantErr error
		wantMsg string
	}{
		{
			name:    "schema version not accepted",
			snap:    func() *snapshot.Snapshot { s := newSnapshot("acme"); s.SchemaVersion = 99; return s },
			wantErr: ErrInvalid,
			wantMsg: "schema version 99",
		},
		{
			name:    "invalid snapshot",
			snap:    func() *snapshot.Snapshot { s := newSnapshot("acme"); s.Host.Hostname = ""; return s },
			wantErr: ErrInvalid,
			wantMsg: "host.hostname is required",
		},
		{
			name: "namespace mismatch",
			snap: func() *snapshot.Snapshot { return newSnapshot("beta") },
			setup: func(m *mocks, _ *mockQueue.MockQueue) {
				m.host.EXPECT().FindByAgentID(ctx, agent.ID).Return(knownHost(), nil)
				m.namespace.EXPECT().FindByID(ctx, namespace.ID).Return(namespace, nil)
			},
			wantErr: ErrNamespaceMismatch,
			wantMsg: `the host is in namespace "acme", the snapshot names "beta"`,
		},
		{
			name: "host lookup fails",
			setup: func(m *mocks, _ *mockQueue.MockQueue) {
				m.host.EXPECT().FindByAgentID(ctx, agent.ID).Return(nil, errDB)
			},
			wantErr: errDB,
		},
		{
			name: "namespace lookup fails",
			setup: func(m *mocks, _ *mockQueue.MockQueue) {
				m.host.EXPECT().FindByAgentID(ctx, agent.ID).Return(knownHost(), nil)
				m.namespace.EXPECT().FindByID(ctx, namespace.ID).Return(nil, errDB)
			},
			wantErr: errDB,
		},
		{
			name: "namespace creation fails",
			setup: func(m *mocks, _ *mockQueue.MockQueue) {
				m.host.EXPECT().FindByAgentID(ctx, agent.ID).Return(nil, gorm.ErrRecordNotFound)
				m.namespace.EXPECT().FindBySlug(ctx, "acme").Return(nil, gorm.ErrRecordNotFound)
				m.namespace.EXPECT().Create(ctx, gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "host creation fails",
			setup: func(m *mocks, _ *mockQueue.MockQueue) {
				m.host.EXPECT().FindByAgentID(ctx, agent.ID).Return(nil, gorm.ErrRecordNotFound)
				m.namespace.EXPECT().FindBySlug(ctx, "acme").Return(namespace, nil)
				m.host.EXPECT().Create(ctx, gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "touch fails",
			setup: func(m *mocks, _ *mockQueue.MockQueue) {
				m.host.EXPECT().FindByAgentID(ctx, agent.ID).Return(knownHost(), nil)
				m.namespace.EXPECT().FindByID(ctx, namespace.ID).Return(namespace, nil)
				m.agent.EXPECT().Touch(ctx, agent.ID, now).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "host update fails",
			setup: func(m *mocks, _ *mockQueue.MockQueue) {
				m.host.EXPECT().FindByAgentID(ctx, agent.ID).Return(knownHost(), nil)
				m.namespace.EXPECT().FindByID(ctx, namespace.ID).Return(namespace, nil)
				m.agent.EXPECT().Touch(ctx, agent.ID, now).Return(nil)
				m.host.EXPECT().Update(ctx, gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "snapshot creation fails",
			setup: func(m *mocks, _ *mockQueue.MockQueue) {
				m.host.EXPECT().FindByAgentID(ctx, agent.ID).Return(knownHost(), nil)
				m.namespace.EXPECT().FindByID(ctx, namespace.ID).Return(namespace, nil)
				m.agent.EXPECT().Touch(ctx, agent.ID, now).Return(nil)
				m.host.EXPECT().Update(ctx, gomock.Any()).Return(nil)
				m.snapshot.EXPECT().Create(ctx, gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "enqueue fails",
			setup: func(m *mocks, q *mockQueue.MockQueue) {
				m.host.EXPECT().FindByAgentID(ctx, agent.ID).Return(knownHost(), nil)
				m.namespace.EXPECT().FindByID(ctx, namespace.ID).Return(namespace, nil)
				m.agent.EXPECT().Touch(ctx, agent.ID, now).Return(nil)
				m.host.EXPECT().Update(ctx, gomock.Any()).Return(nil)
				m.snapshot.EXPECT().Create(ctx, gomock.Any()).Return(nil)
				q.EXPECT().InsertNormalizeSnapshot(ctx, gomock.Any(), gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			q := mockQueue.NewMockQueue(gomock.NewController(t))
			if tt.setup != nil {
				tt.setup(m, q)
			}
			s := newSnapshot("acme")
			if tt.snap != nil {
				s = tt.snap()
			}
			_, err := NewSnapshotService(testContext(t), m.tx, m.agent, q).Ingest(ctx, agent, s, raw)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.ErrorContains(t, err, tt.wantMsg)
		})
	}
}
