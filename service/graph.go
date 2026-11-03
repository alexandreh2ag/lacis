package service

import (
	"context"
	"errors"
	"fmt"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/topology"
	"gorm.io/gorm"
)

var ErrNamespaceNotFound = errors.New("namespace not found")

// GraphService draws the graph of a namespace, for the export and the API.
type GraphService interface {
	// Graph returns the graph of the namespace of this slug, with its links
	// as last computed.
	Graph(ctx context.Context, namespace string, filter topology.Filter) (*topology.Graph, error)
}

type graphService struct {
	appCtx *appContext.Context
	tx     repository.Transactor
}

func NewGraphService(appCtx *appContext.Context, tx repository.Transactor) GraphService {
	return &graphService{appCtx: appCtx, tx: tx}
}

func (s *graphService) Graph(ctx context.Context, namespace string, filter topology.Filter) (*topology.Graph, error) {
	var inv *model.Inventory
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		ns, err := repos.Namespace.FindBySlug(ctx, namespace)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: %s", ErrNamespaceNotFound, namespace)
		}
		if err != nil {
			return err
		}
		inv, err = repos.Inventory.Load(ctx, ns.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return topology.Build(inv, filter)
}
