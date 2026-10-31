package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/hash"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"gorm.io/gorm"
)

// ImportTokenLabel labels the enrollment token the imported agents are
// tied to; it is revoked at creation and enrolls nobody.
const ImportTokenLabel = "snapshot import"

// ImportResult tells what an import did.
type ImportResult struct {
	Namespace string
	Hostname  string
	Result    IngestResult
	// AgentCreated is true for the first import of a host.
	AgentCreated bool
}

// ImportService injects snapshots without agent, for development and
// tests, through the same ingestion as the agent API.
type ImportService interface {
	// Import ingests the snapshot raw as sent by the agent of its host,
	// created on the first import of that host.
	Import(ctx context.Context, raw json.RawMessage) (*ImportResult, error)
}

type importService struct {
	appCtx    *appContext.Context
	repos     *repository.Repositories
	snapshots SnapshotService
}

func NewImportService(appCtx *appContext.Context, repos *repository.Repositories, snapshots SnapshotService) ImportService {
	return &importService{appCtx: appCtx, repos: repos, snapshots: snapshots}
}

func (s *importService) Import(ctx context.Context, raw json.RawMessage) (*ImportResult, error) {
	snap := &snapshot.Snapshot{}
	if err := json.Unmarshal(raw, snap); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	res := &ImportResult{Namespace: snap.Namespace, Hostname: snap.Host.Hostname}
	agent, err := s.agentOf(ctx, snap)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		agent, err = s.newAgent(ctx)
		res.AgentCreated = true
	}
	if err != nil {
		return nil, err
	}
	if res.Result, err = s.snapshots.Ingest(ctx, agent, snap, raw); err != nil {
		return nil, err
	}
	return res, nil
}

// agentOf returns the agent of the host of snap, gorm.ErrRecordNotFound
// when it was never imported.
func (s *importService) agentOf(ctx context.Context, snap *snapshot.Snapshot) (*model.Agent, error) {
	namespace, err := s.repos.Namespace.FindBySlug(ctx, snap.Namespace)
	if err != nil {
		return nil, err
	}
	host, err := s.repos.Host.FindByHostname(ctx, namespace.ID, snap.Host.Hostname)
	if err != nil {
		return nil, err
	}
	return s.repos.Agent.FindByID(ctx, host.AgentID)
}

// newAgent creates an agent whose token nobody knows, tied to the revoked
// import token.
func (s *importService) newAgent(ctx context.Context) (*model.Agent, error) {
	token, err := s.repos.EnrollmentToken.FindByLabel(ctx, ImportTokenLabel)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		token, err = s.newImportToken(ctx)
	}
	if err != nil {
		return nil, err
	}
	_, secretHash, err := hash.NewToken(AgentTokenPrefix)
	if err != nil {
		return nil, err
	}
	agent := &model.Agent{TokenHash: secretHash, EnrollmentTokenID: token.ID}
	if err := s.repos.Agent.Create(ctx, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

// newImportToken creates the import token, revoked at once.
func (s *importService) newImportToken(ctx context.Context) (*model.EnrollmentToken, error) {
	_, secretHash, err := hash.NewToken(EnrollmentTokenPrefix)
	if err != nil {
		return nil, err
	}
	label, now := ImportTokenLabel, s.appCtx.Clock.Now()
	token := &model.EnrollmentToken{TokenHash: secretHash, Label: &label, RevokedAt: &now}
	if err := s.repos.EnrollmentToken.Create(ctx, token); err != nil {
		return nil, err
	}
	return token, nil
}
