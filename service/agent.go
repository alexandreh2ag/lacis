package service

import (
	"context"
	"errors"
	"fmt"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/hash"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AgentTokenPrefix starts every agent token, so that a leaked one is easy to
// recognize.
const AgentTokenPrefix = "lacis_at_"

type EnrolledAgent struct {
	Agent *model.Agent
	// Secret is the token the agent authenticates with; only its hash is
	// stored.
	Secret string
}

type AgentService interface {
	// Enroll creates an agent with the enrollment token secret, and returns
	// ErrUnauthorized when that token is unknown, expired or revoked.
	Enroll(ctx context.Context, enrollmentSecret string) (*EnrolledAgent, error)
	// List returns the agents with their host, the last enrolled first.
	List(ctx context.Context) ([]repository.AgentRow, error)
	// Revoke refuses the token of the agent from now on: its snapshots are
	// refused and it stops. Its host stays until deleted.
	Revoke(ctx context.Context, id uuid.UUID) (*repository.AgentRow, error)
}

type agentService struct {
	appCtx *appContext.Context
	tokens repository.EnrollmentTokenRepository
	agents repository.AgentRepository
}

func NewAgentService(appCtx *appContext.Context, tokens repository.EnrollmentTokenRepository, agents repository.AgentRepository) AgentService {
	return &agentService{appCtx: appCtx, tokens: tokens, agents: agents}
}

func (s *agentService) Enroll(ctx context.Context, enrollmentSecret string) (*EnrolledAgent, error) {
	token, err := s.tokens.FindByHash(ctx, hash.Token(enrollmentSecret))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if !token.Usable(s.appCtx.Clock.Now()) {
		return nil, ErrUnauthorized
	}

	secret, secretHash, err := hash.NewToken(AgentTokenPrefix)
	if err != nil {
		return nil, err
	}
	agent := &model.Agent{TokenHash: secretHash, EnrollmentTokenID: token.ID}
	if err = s.agents.Create(ctx, agent); err != nil {
		return nil, err
	}
	s.appCtx.Logger.Info("agent enrolled", "agent_id", agent.ID, "enrollment_token_id", token.ID)
	return &EnrolledAgent{Agent: agent, Secret: secret}, nil
}

func (s *agentService) List(ctx context.Context) ([]repository.AgentRow, error) {
	return s.agents.FindAll(ctx)
}

func (s *agentService) Revoke(ctx context.Context, id uuid.UUID) (*repository.AgentRow, error) {
	if err := s.agents.Revoke(ctx, id, s.appCtx.Clock.Now()); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: agent %s", ErrNotFound, id)
		}
		return nil, err
	}
	s.appCtx.Logger.Info("agent revoked", "agent_id", id)
	rows, err := s.agents.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i], nil
		}
	}
	return nil, fmt.Errorf("%w: agent %s", ErrNotFound, id)
}
