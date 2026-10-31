package graph

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// DepthLimit refuses an operation nesting its fields deeper than Max, the
// fragments counted at their place.
type DepthLimit struct {
	Max int
}

var _ interface {
	graphql.HandlerExtension
	graphql.OperationContextMutator
} = DepthLimit{}

func (DepthLimit) ExtensionName() string { return "DepthLimit" }

func (DepthLimit) Validate(graphql.ExecutableSchema) error { return nil }

func (d DepthLimit) MutateOperationContext(_ context.Context, opCtx *graphql.OperationContext) *gqlerror.Error {
	op := opCtx.Doc.Operations.ForName(opCtx.OperationName)
	if op == nil {
		return nil
	}
	if depth := selectionDepth(op.SelectionSet, map[string]bool{}); depth > d.Max {
		return gqlerror.Errorf("operation has depth %d, which exceeds the limit of %d", depth, d.Max)
	}
	return nil
}

// selectionDepth is the depth of the deepest field of the set; visiting
// guards against fragment cycles, which the validation refuses anyway.
func selectionDepth(set ast.SelectionSet, visiting map[string]bool) int {
	deepest := 0
	for _, sel := range set {
		depth := 0
		switch s := sel.(type) {
		case *ast.Field:
			depth = 1 + selectionDepth(s.SelectionSet, visiting)
		case *ast.InlineFragment:
			depth = selectionDepth(s.SelectionSet, visiting)
		case *ast.FragmentSpread:
			if s.Definition != nil && !visiting[s.Name] {
				visiting[s.Name] = true
				depth = selectionDepth(s.Definition.SelectionSet, visiting)
				delete(visiting, s.Name)
			}
		}
		deepest = max(deepest, depth)
	}
	return deepest
}
