package service

import (
	"context"
	"testing"

	mockRepository "github.com/alexandreh2ag/lacis/mocks/repository"
	"github.com/alexandreh2ag/lacis/repository"
	"go.uber.org/mock/gomock"
)

var ctx = context.Background()

func ptr[T any](v T) *T { return &v }

// mocks gives a service mocked repositories, reached through a transactor
// that runs the function right away and returns its error.
type mocks struct {
	tx        *mockRepository.MockTransactor
	namespace *mockRepository.MockNamespaceRepository
	token     *mockRepository.MockEnrollmentTokenRepository
	agent     *mockRepository.MockAgentRepository
	host      *mockRepository.MockHostRepository
	snapshot  *mockRepository.MockSnapshotRepository
	user      *mockRepository.MockUserRepository
	role      *mockRepository.MockRoleRepository
	session   *mockRepository.MockSessionRepository
	techIcon  *mockRepository.MockTechIconRepository
	inventory *mockRepository.MockInventoryRepository
}

func newMocks(t *testing.T) *mocks {
	ctrl := gomock.NewController(t)
	m := &mocks{
		tx:        mockRepository.NewMockTransactor(ctrl),
		namespace: mockRepository.NewMockNamespaceRepository(ctrl),
		token:     mockRepository.NewMockEnrollmentTokenRepository(ctrl),
		agent:     mockRepository.NewMockAgentRepository(ctrl),
		host:      mockRepository.NewMockHostRepository(ctrl),
		snapshot:  mockRepository.NewMockSnapshotRepository(ctrl),
		user:      mockRepository.NewMockUserRepository(ctrl),
		role:      mockRepository.NewMockRoleRepository(ctrl),
		session:   mockRepository.NewMockSessionRepository(ctrl),
		techIcon:  mockRepository.NewMockTechIconRepository(ctrl),
		inventory: mockRepository.NewMockInventoryRepository(ctrl),
	}
	repos := &repository.Repositories{
		Namespace:       m.namespace,
		EnrollmentToken: m.token,
		Agent:           m.agent,
		Host:            m.host,
		Snapshot:        m.snapshot,
		User:            m.user,
		Role:            m.role,
		Session:         m.session,
		TechIcon:        m.techIcon,
		Inventory:       m.inventory,
	}
	m.tx.EXPECT().InTx(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, fn func(*repository.Repositories) error) error { return fn(repos) })
	return m
}
