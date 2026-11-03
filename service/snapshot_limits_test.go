package service

import (
	"strings"
	"testing"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
)

func TestCheckSizes(t *testing.T) {
	long := strings.Repeat("x", MaxSnapshotString+1)
	tests := []struct {
		name    string
		mutate  func(s *snapshot.Snapshot)
		wantMsg string
	}{
		{name: "valid", mutate: func(*snapshot.Snapshot) {}},
		{name: "long hostname", mutate: func(s *snapshot.Snapshot) { s.Host.Hostname = long }, wantMsg: "host.hostname holds 8193 bytes"},
		{name: "long nested text", mutate: func(s *snapshot.Snapshot) {
			s.Apps = []snapshot.App{{Path: "/srv/app", Role: snapshot.AppRoleWeb, Name: long}}
		}, wantMsg: "apps[0].name holds"},
		{name: "too many items", mutate: func(s *snapshot.Snapshot) {
			s.Apps = make([]snapshot.App, MaxSnapshotItems+1)
		}, wantMsg: "apps holds 10001 items"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSnapshot("acme")
			tt.mutate(s)
			err := checkSizes(s)
			if tt.wantMsg == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, ErrInvalid)
			assert.ErrorContains(t, err, tt.wantMsg)
		})
	}
}

func TestCheckSizes_Maps(t *testing.T) {
	assert.NoError(t, checkSizes(map[string]*string{"a": nil}))
	assert.ErrorContains(t, checkSizes(map[string]string{"k": strings.Repeat("x", MaxSnapshotString+1)}), "k holds")
	assert.ErrorContains(t, checkSizes(map[string]int{strings.Repeat("x", MaxSnapshotString+1): 1}), "<key> holds")
	big := make(map[int]int, MaxSnapshotItems+1)
	for i := range MaxSnapshotItems + 1 {
		big[i] = i
	}
	assert.ErrorContains(t, checkSizes(big), "snapshot holds 10001 items")
	assert.NoError(t, checkSizes(struct{ Raw []byte }{Raw: make([]byte, MaxSnapshotString+1)}), "bytes are bounded by the body")
}
