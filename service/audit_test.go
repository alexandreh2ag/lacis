package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestAuditDetails(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "nil", in: nil, want: `{}`},
		{name: "no object", in: []string{"a"}, want: `{}`},
		{name: "unmarshalable", in: map[string]any{"f": func() {}}, want: `{}`},
		{name: "arguments", in: map[string]any{"namespace": "acme", "id": 3}, want: `{"namespace":"acme","id":3}`},
		{name: "passwords and secrets left out, at any depth",
			in: map[string]any{"input": map[string]any{"username": "bob", "password": "p", "list": []any{map[string]any{"clientSecret": "s"}}},
				"currentPassword": "c", "newPassword": "n"},
			want: `{"input":{"username":"bob","password":"***","list":[{"clientSecret":"***"}]},"currentPassword":"***","newPassword":"***"}`},
		{name: "a struct", in: struct {
			Host string `json:"host"`
		}{Host: "web-1"}, want: `{"host":"web-1"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.JSONEq(t, tt.want, string(auditDetails(tt.in)))
		})
	}
}

func TestAuditService_Record(t *testing.T) {
	m := newMocks(t)
	user := model.User{Base: model.Base{ID: uuid.New()}, Username: "bob"}
	m.audit.EXPECT().Create(gomock.Any(), &model.AuditEvent{At: now, UserID: &user.ID, Username: "bob", Source: model.AuditMCP,
		Action: "get_host", Namespace: new("acme"), Target: new("web-1"), Details: json.RawMessage(`{"host":"web-1"}`)}).Return(nil)
	svc := NewAuditService(testContext(t), m.audit)
	svc.Record(ctx, user, model.AuditMCP, "get_host", AuditAbout{Namespace: " acme ", Target: "web-1"}, map[string]string{"host": "web-1"})

	// A user without id (none such) and a failure: logged, never returned.
	var logs bytes.Buffer
	appCtx := testContext(t)
	appCtx.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	m.audit.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, e *model.AuditEvent) error {
		assert.Nil(t, e.UserID)
		assert.Nil(t, e.Namespace, "an empty about is unknown")
		return errors.New("db down")
	})
	NewAuditService(appCtx, m.audit).Record(ctx, model.User{Username: "ghost"}, model.AuditUI, "deleteHost", AuditAbout{}, nil)
	assert.Contains(t, logs.String(), "audit event not recorded")
}

func TestAuditService_Read(t *testing.T) {
	m := newMocks(t)
	filter := repository.AuditFilter{Username: "bob", Limit: 10}
	m.audit.EXPECT().List(ctx, filter).Return([]model.AuditEvent{{Action: "login"}}, int64(12), nil)
	m.audit.EXPECT().Usernames(ctx, "bo", MaxAuditUsernames).Return([]string{"bob"}, nil)
	m.audit.EXPECT().Actions(ctx, "host", MaxAuditUsernames).Return([]string{"get_host"}, nil)
	svc := NewAuditService(testContext(t), m.audit)
	events, total, err := svc.List(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, int64(12), total)
	assert.Len(t, events, 1)
	names, err := svc.Usernames(ctx, "bo")
	require.NoError(t, err)
	assert.Equal(t, []string{"bob"}, names)
	actions, err := svc.Actions(ctx, "host")
	require.NoError(t, err)
	assert.Equal(t, []string{"get_host"}, actions)
}
