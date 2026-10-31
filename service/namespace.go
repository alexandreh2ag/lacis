package service

import (
	"context"
	"errors"
	"fmt"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NamespaceService reads the namespaces and their inventory, for the API.
type NamespaceService interface {
	// List returns every namespace, by name.
	List(ctx context.Context) ([]model.Namespace, error)
	// BySlug returns the namespace of the slug; ErrNamespaceNotFound when
	// unknown.
	BySlug(ctx context.Context, slug string) (*model.Namespace, error)
	// Inventory returns the current content of the namespace, with its
	// links as last computed.
	Inventory(ctx context.Context, namespaceID uuid.UUID) (*model.Inventory, error)
}

type namespaceService struct {
	appCtx *appContext.Context
	tx     repository.Transactor
}

func NewNamespaceService(appCtx *appContext.Context, tx repository.Transactor) NamespaceService {
	return &namespaceService{appCtx: appCtx, tx: tx}
}

func (s *namespaceService) List(ctx context.Context) ([]model.Namespace, error) {
	var namespaces []model.Namespace
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		namespaces, err = repos.Namespace.FindAll(ctx)
		return err
	})
	return namespaces, err
}

func (s *namespaceService) BySlug(ctx context.Context, slug string) (*model.Namespace, error) {
	var ns *model.Namespace
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		ns, err = repos.Namespace.FindBySlug(ctx, slug)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: %s", ErrNamespaceNotFound, slug)
		}
		return err
	})
	return ns, err
}

func (s *namespaceService) Inventory(ctx context.Context, namespaceID uuid.UUID) (*model.Inventory, error) {
	var inv *model.Inventory
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		inv, err = repos.Inventory.Load(ctx, namespaceID)
		return err
	})
	return inv, err
}
