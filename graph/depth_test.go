package graph

import (
	"context"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

func TestDepthLimit(t *testing.T) {
	schema := gqlparser.MustLoadSchema(&ast.Source{Input: `
		type Query { a: A }
		type A { b: A, c: String }
	`})
	op := func(query string) *graphql.OperationContext {
		doc, errs := gqlparser.LoadQueryWithRules(schema, query, nil)
		if errs != nil {
			t.Fatal(errs)
		}
		return &graphql.OperationContext{Doc: doc}
	}
	d := DepthLimit{Max: 3}
	assert.Equal(t, "DepthLimit", d.ExtensionName())
	assert.NoError(t, d.Validate(nil))
	assert.Nil(t, d.MutateOperationContext(context.Background(), op(`{ a { b { c } } }`)))
	assert.Nil(t, d.MutateOperationContext(context.Background(), op(`{ a { ...F } } fragment F on A { b { c } }`)))
	assert.EqualError(t, d.MutateOperationContext(context.Background(), op(`{ a { b { ... on A { b { c } } } } }`)),
		"input: operation has depth 4, which exceeds the limit of 3")
	assert.Nil(t, d.MutateOperationContext(context.Background(), &graphql.OperationContext{Doc: &ast.QueryDocument{}, OperationName: "none"}))
}
