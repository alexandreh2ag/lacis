package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	mockRepository "github.com/alexandreh2ag/lacis/mocks/repository"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"
)

type importMocks struct {
	namespace *mockRepository.MockNamespaceRepository
	host      *mockRepository.MockHostRepository
	agent     *mockRepository.MockAgentRepository
	token     *mockRepository.MockEnrollmentTokenRepository
	snapshots *fakeIngest
	svc       ImportService
}

// fakeIngest stands for the snapshot service: the mocks of this package
// cannot be imported by its own tests.
type fakeIngest struct {
	SnapshotService
	t      *testing.T
	agent  *model.Agent
	raw    json.RawMessage
	result IngestResult
	err    error
	called bool
}

func (f *fakeIngest) Ingest(_ context.Context, agent *model.Agent, s *snapshot.Snapshot, raw json.RawMessage) (IngestResult, error) {
	f.called = true
	assert.Equal(f.t, "acme-site-prod-1", s.Host.Hostname)
	if f.agent != nil {
		assert.Equal(f.t, f.agent, agent)
	}
	if f.raw != nil {
		assert.Equal(f.t, f.raw, raw)
	}
	return f.result, f.err
}

func newImportMocks(t *testing.T) *importMocks {
	ctrl := gomock.NewController(t)
	m := &importMocks{
		namespace: mockRepository.NewMockNamespaceRepository(ctrl),
		host:      mockRepository.NewMockHostRepository(ctrl),
		agent:     mockRepository.NewMockAgentRepository(ctrl),
		token:     mockRepository.NewMockEnrollmentTokenRepository(ctrl),
		snapshots: &fakeIngest{t: t},
	}
	repos := &repository.Repositories{Namespace: m.namespace, Host: m.host, Agent: m.agent, EnrollmentToken: m.token}
	m.svc = NewImportService(testContext(t), repos, m.snapshots)
	return m
}

func importRaw(t *testing.T) json.RawMessage {
	b, err := json.Marshal(newSnapshot("acme"))
	require.NoError(t, err)
	return b
}

func TestImportService_KnownHost(t *testing.T) {
	m := newImportMocks(t)
	ns := &model.Namespace{Base: model.Base{ID: uuid.New()}}
	agent := &model.Agent{Base: model.Base{ID: uuid.New()}}
	raw := importRaw(t)
	m.namespace.EXPECT().FindBySlug(ctx, "acme").Return(ns, nil)
	m.host.EXPECT().FindByHostname(ctx, ns.ID, "acme-site-prod-1").Return(&model.Host{AgentID: agent.ID}, nil)
	m.agent.EXPECT().FindByID(ctx, agent.ID).Return(agent, nil)
	m.snapshots.agent, m.snapshots.raw, m.snapshots.result = agent, raw, Unchanged

	got, err := m.svc.Import(ctx, raw)
	require.NoError(t, err)
	assert.Equal(t, &ImportResult{Namespace: "acme", Hostname: "acme-site-prod-1", Result: Unchanged}, got)
}

func TestImportService_NewHost(t *testing.T) {
	for _, tokenExists := range []bool{true, false} {
		t.Run(map[bool]string{true: "import token exists", false: "first import"}[tokenExists], func(t *testing.T) {
			m := newImportMocks(t)
			token := &model.EnrollmentToken{Base: model.Base{ID: uuid.New()}}
			m.namespace.EXPECT().FindBySlug(ctx, "acme").Return(nil, gorm.ErrRecordNotFound)
			if tokenExists {
				m.token.EXPECT().FindByLabel(ctx, ImportTokenLabel).Return(token, nil)
			} else {
				m.token.EXPECT().FindByLabel(ctx, ImportTokenLabel).Return(nil, gorm.ErrRecordNotFound)
				m.token.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ any, tk *model.EnrollmentToken) error {
					assert.Equal(t, ImportTokenLabel, *tk.Label)
					assert.Equal(t, now, *tk.RevokedAt, "revoked: it enrolls nobody")
					tk.ID = token.ID
					return nil
				})
			}
			m.agent.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ any, a *model.Agent) error {
				assert.Equal(t, token.ID, a.EnrollmentTokenID)
				return nil
			})
			m.snapshots.result = Accepted

			got, err := m.svc.Import(ctx, importRaw(t))
			require.NoError(t, err)
			assert.True(t, got.AgentCreated)
			assert.Equal(t, Accepted, got.Result)
		})
	}
}

func TestImportService_Errors(t *testing.T) {
	errDB := errors.New("db down")
	tests := []struct {
		name    string
		raw     string
		setup   func(m *importMocks)
		wantErr error
	}{
		{name: "not json", raw: "{", wantErr: ErrInvalid},
		{
			name:    "lookup fails",
			setup:   func(m *importMocks) { m.namespace.EXPECT().FindBySlug(ctx, "acme").Return(nil, errDB) },
			wantErr: errDB,
		},
		{
			name: "token fails",
			setup: func(m *importMocks) {
				m.namespace.EXPECT().FindBySlug(ctx, "acme").Return(nil, gorm.ErrRecordNotFound)
				m.token.EXPECT().FindByLabel(ctx, ImportTokenLabel).Return(nil, errDB)
			},
			wantErr: errDB,
		},
		{
			name: "agent fails",
			setup: func(m *importMocks) {
				m.namespace.EXPECT().FindBySlug(ctx, "acme").Return(nil, gorm.ErrRecordNotFound)
				m.token.EXPECT().FindByLabel(ctx, ImportTokenLabel).Return(&model.EnrollmentToken{}, nil)
				m.agent.EXPECT().Create(ctx, gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "ingestion fails",
			setup: func(m *importMocks) {
				ns := &model.Namespace{}
				m.namespace.EXPECT().FindBySlug(ctx, "acme").Return(ns, nil)
				m.host.EXPECT().FindByHostname(ctx, ns.ID, gomock.Any()).Return(nil, gorm.ErrRecordNotFound)
				m.token.EXPECT().FindByLabel(ctx, ImportTokenLabel).Return(&model.EnrollmentToken{}, nil)
				m.agent.EXPECT().Create(ctx, gomock.Any()).Return(nil)
				m.snapshots.err = ErrInvalid
			},
			wantErr: ErrInvalid,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newImportMocks(t)
			if tt.setup != nil {
				tt.setup(m)
			}
			raw := json.RawMessage(tt.raw)
			if tt.raw == "" {
				raw = importRaw(t)
			}
			_, err := m.svc.Import(ctx, raw)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}
