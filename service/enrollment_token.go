package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/hash"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EnrollmentTokenPrefix starts every enrollment token, so that a leaked one
// is easy to recognize.
const EnrollmentTokenPrefix = "lacis_et_"

type CreateEnrollmentTokenInput struct {
	Label string
	// TTL is the token lifetime; zero means it never expires.
	TTL time.Duration
}

type CreatedEnrollmentToken struct {
	Token *model.EnrollmentToken
	// Secret is the token to give to the agents; only its hash is stored.
	Secret string
}

// EnrollmentTokenDetail is a token and how many agents it enrolled.
type EnrollmentTokenDetail struct {
	model.EnrollmentToken
	Agents int
}

type EnrollmentTokenService interface {
	Create(ctx context.Context, in CreateEnrollmentTokenInput) (*CreatedEnrollmentToken, error)
	// List returns the tokens, the last created first.
	List(ctx context.Context) ([]EnrollmentTokenDetail, error)
	// Revoke stops the token from enrolling agents; the agents it enrolled
	// stay.
	Revoke(ctx context.Context, id uuid.UUID) (*EnrollmentTokenDetail, error)
}

type enrollmentTokenService struct {
	appCtx *appContext.Context
	repo   repository.EnrollmentTokenRepository
	agents repository.AgentRepository
}

func NewEnrollmentTokenService(appCtx *appContext.Context, repo repository.EnrollmentTokenRepository, agents repository.AgentRepository) EnrollmentTokenService {
	return &enrollmentTokenService{appCtx: appCtx, repo: repo, agents: agents}
}

func (s *enrollmentTokenService) Create(ctx context.Context, in CreateEnrollmentTokenInput) (*CreatedEnrollmentToken, error) {
	if in.TTL < 0 {
		return nil, fmt.Errorf("%w: the token lifetime cannot be negative", ErrInvalid)
	}
	secret, secretHash, err := hash.NewToken(EnrollmentTokenPrefix)
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	token := &model.EnrollmentToken{TokenHash: secretHash}
	if in.TTL > 0 {
		expiresAt := s.appCtx.Clock.Now().Add(in.TTL)
		token.ExpiresAt = &expiresAt
	}
	if label := strings.TrimSpace(in.Label); label != "" {
		token.Label = &label
	}
	if err = s.repo.Create(ctx, token); err != nil {
		return nil, err
	}
	return &CreatedEnrollmentToken{Token: token, Secret: secret}, nil
}

func (s *enrollmentTokenService) List(ctx context.Context) ([]EnrollmentTokenDetail, error) {
	tokens, err := s.repo.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.agents.CountByEnrollmentToken(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]EnrollmentTokenDetail, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, EnrollmentTokenDetail{EnrollmentToken: t, Agents: counts[t.ID]})
	}
	return out, nil
}

func (s *enrollmentTokenService) Revoke(ctx context.Context, id uuid.UUID) (*EnrollmentTokenDetail, error) {
	if err := s.repo.Revoke(ctx, id, s.appCtx.Clock.Now()); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: enrollment token %s", ErrNotFound, id)
		}
		return nil, err
	}
	token, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	counts, err := s.agents.CountByEnrollmentToken(ctx)
	if err != nil {
		return nil, err
	}
	return &EnrollmentTokenDetail{EnrollmentToken: *token, Agents: counts[id]}, nil
}
