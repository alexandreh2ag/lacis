package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/repository"
	"gorm.io/gorm"
)

var ErrHostNotFound = errors.New("host not found")

// HistoryFilter selects the change events of a namespace.
type HistoryFilter struct {
	// Hostname and EntityType keep the events of a host, of a kind of
	// entity; empty keeps all.
	Hostname   string
	EntityType string
	// Since keeps the events from this time on; zero keeps all.
	Since time.Time
	// OldestFirst lists the events in the order they occurred, the latest
	// first otherwise.
	OldestFirst bool
	// Offset skips the first events in that order; Limit is the number of
	// events returned, zero returning all.
	Offset int
	Limit  int
}

// HistoryService reads the change events, for the CLI and the API.
type HistoryService interface {
	// List returns the change events of the namespace of this slug, the
	// latest first unless filter.OldestFirst.
	List(ctx context.Context, namespace string, filter HistoryFilter) ([]repository.ChangeEventRow, error)
}

type historyService struct {
	appCtx *appContext.Context
	tx     repository.Transactor
}

func NewHistoryService(appCtx *appContext.Context, tx repository.Transactor) HistoryService {
	return &historyService{appCtx: appCtx, tx: tx}
}

func (s *historyService) List(ctx context.Context, namespace string, filter HistoryFilter) ([]repository.ChangeEventRow, error) {
	var rows []repository.ChangeEventRow
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		ns, err := repos.Namespace.FindBySlug(ctx, namespace)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: %s", ErrNamespaceNotFound, namespace)
		}
		if err != nil {
			return err
		}
		f := repository.ChangeEventFilter{NamespaceID: ns.ID, EntityType: filter.EntityType, Since: filter.Since,
			OldestFirst: filter.OldestFirst, Offset: filter.Offset, Limit: filter.Limit}
		if filter.Hostname != "" {
			host, err := repos.Host.FindByHostname(ctx, ns.ID, filter.Hostname)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return refuse(ErrHostNotFound, ReasonHostNotFound, map[string]any{"hostname": filter.Hostname}, "%s", filter.Hostname)
			}
			if err != nil {
				return err
			}
			f.HostID = &host.ID
		}
		rows, err = repos.ChangeEvent.List(ctx, f)
		return err
	})
	return rows, err
}
