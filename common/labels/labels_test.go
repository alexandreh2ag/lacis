package labels

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLabels_Validate(t *testing.T) {
	tests := []struct {
		name    string
		labels  Labels
		wantErr []string
	}{
		{
			name:   "required only",
			labels: Labels{Namespace: "acme", Project: "site", Env: "prod"},
		},
		{
			name:   "all labels",
			labels: Labels{Namespace: "acme", Project: "site-v2", Env: "pre_prod", Index: "1", Role: "Web"},
		},
		{
			name:    "missing required",
			labels:  Labels{Project: "site"},
			wantErr: []string{"namespace: label is required", "env: label is required"},
		},
		{
			name:    "upper case slug",
			labels:  Labels{Namespace: "Acme", Project: "site", Env: "prod"},
			wantErr: []string{`namespace "Acme": label is invalid`},
		},
		{
			name:    "invalid characters",
			labels:  Labels{Namespace: "acme", Project: "my site", Env: "prod", Role: "-web"},
			wantErr: []string{`project "my site": label is invalid`, `role "-web": label is invalid`},
		},
		{
			name:    "too long",
			labels:  Labels{Namespace: "acme", Project: "site", Env: strings.Repeat("a", MaxLength+1)},
			wantErr: []string{"env", "label is invalid"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.labels.Validate()
			if len(tt.wantErr) == 0 {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
			for _, want := range tt.wantErr {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

func TestLabels_ValidateWrapsSentinels(t *testing.T) {
	err := Labels{Namespace: "UPPER"}.Validate()
	assert.ErrorIs(t, err, ErrRequired)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestIsSlug(t *testing.T) {
	assert.True(t, IsSlug("acme"))
	assert.True(t, IsSlug("a"))
	assert.False(t, IsSlug(""))
	assert.False(t, IsSlug("Acme"))
	assert.False(t, IsSlug("site-"))
}
