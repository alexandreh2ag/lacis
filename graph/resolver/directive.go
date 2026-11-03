package resolver

import (
	"context"
	"errors"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// The codes of the errors a client can act on, in their extensions.
const (
	CodeNotFound  = "NOT_FOUND"
	CodeForbidden = "FORBIDDEN"
	CodeInvalid   = "BAD_USER_INPUT"
	CodeConflict  = "CONFLICT"
	// CodeGraphTooLarge refuses a graph of more nodes than graph.max_nodes:
	// the client filters it.
	CodeGraphTooLarge = "GRAPH_TOO_LARGE"
	// CodeUnauthenticated refuses a request without session.
	CodeUnauthenticated = "UNAUTHENTICATED"
	// CodeInternal hides an error the client cannot act on, e.g. an SQL
	// error: the server logs it.
	CodeInternal = "INTERNAL_SERVER_ERROR"
)

// ErrorPresenter returns the errors coded for the client, and those of
// GraphQL itself (parse, validation, limits), as they are; it logs any
// other and hides it behind CodeInternal, so that no SQL error or internal
// detail reaches the client.
func (r *Resolver) ErrorPresenter(ctx context.Context, err error) *gqlerror.Error {
	var gqlErr *gqlerror.Error
	if errors.As(err, &gqlErr) && (gqlErr.Err == nil || gqlErr.Extensions["code"] != nil) {
		return gqlErr
	}
	path := graphql.GetPath(ctx)
	if errors.Is(err, ErrUnauthenticated) {
		return &gqlerror.Error{Path: path, Message: "unauthenticated", Extensions: map[string]any{"code": CodeUnauthenticated}}
	}
	if r.Logger != nil {
		r.Logger.ErrorContext(ctx, "GraphQL resolver", "path", path.String(), "error", err)
	}
	return &gqlerror.Error{Path: path, Message: "internal error", Extensions: map[string]any{"code": CodeInternal}}
}

func coded(ctx context.Context, code, message string) error {
	return &gqlerror.Error{Path: graphql.GetPath(ctx), Message: message, Extensions: map[string]any{"code": code}}
}

// codedRefusal is a coded error telling, besides its code, the reason of
// the refusal and the values it names, for a client to say it in its
// language: extensions {code, reason, args}.
func codedRefusal(ctx context.Context, code string, err error) error {
	e := &gqlerror.Error{Path: graphql.GetPath(ctx), Message: err.Error(), Extensions: map[string]any{"code": code}}
	var refusal *service.Refusal
	switch {
	case errors.As(err, &refusal):
		e.Extensions["reason"] = refusal.Reason
		if len(refusal.Args) > 0 {
			e.Extensions["args"] = refusal.Args
		}
	case errors.Is(err, service.ErrLastAdmin):
		e.Extensions["reason"] = service.ReasonLastAdmin
	}
	return e
}

// Can is the @can directive: the user must hold the permission in the
// namespace of the namespace argument. A namespace the user cannot read
// reads as unknown, as in Query.namespace.
func (r *Resolver) Can(ctx context.Context, _ any, next graphql.Resolver, resource graph.PermissionResource, action graph.PermissionAction) (any, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	slug, _ := graphql.GetFieldContext(ctx).Args["namespace"].(string)
	ns, err := r.NamespaceService.BySlug(ctx, slug)
	switch {
	case errors.Is(err, service.ErrNamespaceNotFound):
		return nil, coded(ctx, CodeNotFound, "namespace not found")
	case err != nil:
		return nil, err
	case !p.Can(ns.ID, model.ResourceInventory, model.ActionRead):
		return nil, coded(ctx, CodeNotFound, "namespace not found")
	case !p.Can(ns.ID, model.Resource(strings.ToLower(string(resource))), model.Action(strings.ToLower(string(action)))):
		return nil, coded(ctx, CodeForbidden, "forbidden: "+strings.ToLower(string(resource))+" "+strings.ToLower(string(action))+" is needed")
	}
	return next(ctx)
}

// CanGlobal is the @canGlobal directive: the user must hold the permission
// on the resource belonging to no namespace.
func (r *Resolver) CanGlobal(ctx context.Context, _ any, next graphql.Resolver, resource graph.GlobalResource, action graph.PermissionAction) (any, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	res, act := strings.ToLower(string(resource)), strings.ToLower(string(action))
	if !p.CanGlobal(model.Resource(res), model.Action(act)) {
		return nil, coded(ctx, CodeForbidden, "forbidden: "+res+" "+act+" is needed")
	}
	return next(ctx)
}

// userError turns the refusals of a service into coded errors, after
// forgetting the inventory loaded for the request: it may have changed.
func (r *Resolver) userError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrInvalid):
		return codedRefusal(ctx, CodeInvalid, err)
	case errors.Is(err, service.ErrNotFound), errors.Is(err, service.ErrNamespaceNotFound), errors.Is(err, service.ErrHostNotFound):
		return codedRefusal(ctx, CodeNotFound, err)
	case errors.Is(err, service.ErrHostActive):
		return coded(ctx, CodeConflict, "the host is active: its next snapshot would bring it back")
	case errors.Is(err, service.ErrAlreadyExists), errors.Is(err, service.ErrLastAdmin):
		return codedRefusal(ctx, CodeConflict, err)
	case errors.Is(err, service.ErrForbidden):
		return codedRefusal(ctx, CodeForbidden, err)
	}
	return err
}
