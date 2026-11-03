// Package auth tells who makes a request of the front and what they may
// do.
package auth

import (
	"context"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
)

// Principal is the user of a session, with the permissions of their roles.
type Principal struct {
	User      model.User
	SessionID string
	Namespace []model.NamespacePermission
	Global    []model.GlobalPermission
}

// Can reports whether the principal may do action on resource in the
// namespace.
func (p *Principal) Can(namespaceID uuid.UUID, resource model.Resource, action model.Action) bool {
	if p == nil {
		return false
	}
	for _, perm := range p.Namespace {
		if perm.Allows(namespaceID, resource, action) {
			return true
		}
	}
	return false
}

// CanGlobal reports whether the principal may do action on a resource
// belonging to no namespace.
func (p *Principal) CanGlobal(resource model.Resource, action model.Action) bool {
	if p == nil {
		return false
	}
	for _, perm := range p.Global {
		if perm.Allows(resource, action) {
			return true
		}
	}
	return false
}

// CanAnyNamespace reports whether the principal may do action on resource
// in every namespace: the namespaces it may see are then all of them.
func (p *Principal) CanAnyNamespace(resource model.Resource, action model.Action) bool {
	if p == nil {
		return false
	}
	for _, perm := range p.Namespace {
		if perm.NamespaceID == nil && perm.Allows(uuid.Nil, resource, action) {
			return true
		}
	}
	return false
}

// NamespaceIDs are the namespaces in which the principal may do action on
// resource by a permission naming them; CanAnyNamespace tells the others.
func (p *Principal) NamespaceIDs(resource model.Resource, action model.Action) []uuid.UUID {
	if p == nil {
		return nil
	}
	var ids []uuid.UUID
	for _, perm := range p.Namespace {
		if perm.NamespaceID != nil && perm.Allows(*perm.NamespaceID, resource, action) {
			ids = append(ids, *perm.NamespaceID)
		}
	}
	return ids
}

type principalKey struct{}

// WithPrincipal returns ctx carrying the principal.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the principal of the request; nil when anonymous.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}
