package resolver

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
)

// auditSortColumns are the columns of the sorts of the audit events.
var auditSortColumns = map[graph.AuditSortField]string{
	graph.AuditSortFieldAt: repository.AuditSortAt, graph.AuditSortFieldUser: repository.AuditSortUsername,
	graph.AuditSortFieldSource: repository.AuditSortSource, graph.AuditSortFieldAction: repository.AuditSortAction,
	graph.AuditSortFieldNamespace: repository.AuditSortNamespace, graph.AuditSortFieldTarget: repository.AuditSortTarget,
}

type auditKey struct{}

// auditNote is what a resolver tells the audit of its mutation, that the
// arguments and the result do not: the name of what it deleted.
type auditNote struct{ target string }

// auditTarget names what the mutation of ctx was done to, for its audit
// event: a resolver deleting something reads its name before.
func auditTarget(ctx context.Context, target string) {
	if n, ok := ctx.Value(auditKey{}).(*auditNote); ok {
		n.target = target
	}
}

// AuditMutations records each mutation that succeeds in the audit: who,
// the mutation, what it was done to and its arguments, passwords left out.
// A refused or failed one is not recorded.
func (r *Resolver) AuditMutations(ctx context.Context, next graphql.Resolver) (any, error) {
	fc := graphql.GetFieldContext(ctx)
	if r.AuditService == nil || fc == nil || fc.Object != "Mutation" {
		return next(ctx)
	}
	note := &auditNote{}
	res, err := next(context.WithValue(ctx, auditKey{}, note))
	p := auth.FromContext(ctx)
	if err != nil || p == nil {
		return res, err
	}
	about := mutationAbout(fc.Field.Name, fc.Args, res)
	if note.target != "" {
		about.Target = note.target
	}
	r.AuditService.Record(ctx, p.User, model.AuditUI, fc.Field.Name, about, fc.Args)
	return res, err
}

// mutationAbout names what the mutation was done to: the namespace and
// the slug of its arguments, or the name its result gives.
func mutationAbout(field string, args map[string]any, res any) service.AuditAbout {
	str := func(k string) string {
		s, _ := args[k].(string)
		return s
	}
	about := service.AuditAbout{Namespace: str("namespace")}
	switch field {
	case "updateProject", "deleteProject":
		about.Project = str("slug")
	case "runTask":
		about.Target = str("kind")
	default:
		about.Target = str("slug")
	}
	switch x := res.(type) {
	case *service.Account:
		about.Target = x.Username
	case *service.RoleDetail:
		about.Target = x.Code
	case *service.EnrollmentTokenDetail:
		about.Target = deref(x.Label)
	case *graph.CreatedEnrollmentToken:
		if x.Token != nil {
			about.Target = deref(x.Token.Label)
		}
	case *repository.AgentRow:
		about.Target = deref(x.Hostname)
	case *service.TaskRun:
		about.Target = x.Kind
	}
	return about
}
