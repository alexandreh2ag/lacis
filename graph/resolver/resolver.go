// Package resolver resolves the GraphQL API of the front. Every query goes
// through Query.namespace, which checks that the user may read it; the
// content of a namespace is then read from its inventory, loaded once per
// request.
package resolver

import (
	"context"
	"errors"

	"github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/common/clock"
	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/service"
)

// This file will not be regenerated automatically.

// ErrUnauthenticated refuses a request without session; the HTTP
// middleware refuses them before.
var ErrUnauthenticated = errors.New("unauthenticated")

const (
	// DefaultLimit and MaxLimit bound a page of a listing.
	DefaultLimit = 50
	MaxLimit     = 500
)

type Resolver struct {
	Clock            clock.Clock
	NamespaceService service.NamespaceService
	HistoryService   service.HistoryService
	MetadataService  service.MetadataService

	UserService            service.UserService
	RoleService            service.RoleService
	EnrollmentTokenService service.EnrollmentTokenService
	AgentService           service.AgentService
	TechIconService        service.TechIconService
	TaskService            service.TaskService
}

// principal is the user of the request.
func principal(ctx context.Context) (*auth.Principal, error) {
	p := auth.FromContext(ctx)
	if p == nil {
		return nil, ErrUnauthenticated
	}
	return p, nil
}

// bounds reads a page: its offset and its limit, capped at MaxLimit.
func bounds(page *graph.PageInput) (offset, limit int) {
	offset, limit = 0, DefaultLimit
	if page != nil {
		if page.Offset != nil && *page.Offset > 0 {
			offset = *page.Offset
		}
		if page.Limit != nil && *page.Limit > 0 {
			limit = min(*page.Limit, MaxLimit)
		}
	}
	return offset, limit
}

// paginate returns the page of items, and their total.
func paginate[T any](items []T, page *graph.PageInput) ([]T, int) {
	offset, limit := bounds(page)
	total := len(items)
	if offset >= total {
		return []T{}, total
	}
	return items[offset:min(offset+limit, total)], total
}
