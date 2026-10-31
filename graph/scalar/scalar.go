// Package scalar maps the GraphQL scalars to the Go types of the models.
package scalar

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/99designs/gqlgen/graphql"
	"github.com/google/uuid"
)

// MarshalUUID writes an id.
func MarshalUUID(id uuid.UUID) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		_, _ = io.WriteString(w, strconv.Quote(id.String()))
	})
}

// UnmarshalUUID reads an id.
func UnmarshalUUID(v any) (uuid.UUID, error) {
	s, ok := v.(string)
	if !ok {
		return uuid.Nil, fmt.Errorf("an id is a string, got %T", v)
	}
	return uuid.Parse(s)
}

// MarshalJSON writes a JSON value as is; null when empty.
func MarshalJSON(raw json.RawMessage) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		if len(raw) == 0 {
			_, _ = io.WriteString(w, "null")
			return
		}
		_, _ = w.Write(raw)
	})
}

// UnmarshalJSON reads any JSON value.
func UnmarshalJSON(v any) (json.RawMessage, error) {
	return json.Marshal(v)
}
