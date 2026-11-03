package scalar

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUUID(t *testing.T) {
	id := uuid.MustParse("019a2400-0000-7000-8000-0000000000aa")
	var b bytes.Buffer
	MarshalUUID(id).MarshalGQL(&b)
	assert.Equal(t, `"019a2400-0000-7000-8000-0000000000aa"`, b.String())
	got, err := UnmarshalUUID(id.String())
	require.NoError(t, err)
	assert.Equal(t, id, got)
	_, err = UnmarshalUUID(12)
	assert.EqualError(t, err, "an id is a string, got int")
	_, err = UnmarshalUUID("nope")
	assert.Error(t, err)
}

func TestJSON(t *testing.T) {
	var b bytes.Buffer
	MarshalJSON(json.RawMessage(`{"a":1}`)).MarshalGQL(&b)
	assert.Equal(t, `{"a":1}`, b.String())
	b.Reset()
	MarshalJSON(nil).MarshalGQL(&b)
	assert.Equal(t, "null", b.String())
	got, err := UnmarshalJSON(map[string]any{"a": 1})
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":1}`, string(got))
}
