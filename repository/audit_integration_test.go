//go:build integration

package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditRepository(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.Audit
	bob := &model.User{Username: "bob"}
	require.NoError(t, f.repos.User.Create(ctx, bob))
	day := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	str := func(s string) *string { return &s }
	events := []*model.AuditEvent{
		{At: day.Add(1 * time.Hour), UserID: &bob.ID, Username: "bob", Source: model.AuditAuth, Action: "login"},
		{At: day.Add(2 * time.Hour), UserID: &bob.ID, Username: "bob", Source: model.AuditMCP, Action: "get_host",
			Namespace: str("acme"), Target: str("web-1"), Details: json.RawMessage(`{"host":"web-1"}`)},
		{At: day.Add(3 * time.Hour), Username: "Alice", Source: model.AuditUI, Action: "deleteHost",
			Namespace: str("acme"), Target: str("db_1"), Details: json.RawMessage(`{"id":"x"}`)},
		{At: day.Add(26 * time.Hour), Username: "alice", Source: model.AuditMCP, Action: "list_projects", Project: str("shop")},
	}
	for _, e := range events {
		require.NoError(t, repo.Create(ctx, e))
	}

	actions := func(filter AuditFilter) ([]string, int64) {
		t.Helper()
		rows, total, err := repo.List(ctx, filter)
		require.NoError(t, err)
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = r.Action
		}
		return out, total
	}
	tests := []struct {
		name   string
		filter AuditFilter
		want   []string
		total  int64
	}{
		{name: "all, newest first", want: []string{"list_projects", "deleteHost", "get_host", "login"}, total: 4},
		{name: "a day", filter: AuditFilter{From: day, To: day.Add(24 * time.Hour)}, want: []string{"deleteHost", "get_host", "login"}, total: 3},
		{name: "a user, whatever its case", filter: AuditFilter{Username: "ALICE"}, want: []string{"list_projects", "deleteHost"}, total: 2},
		{name: "a part of a username", filter: AuditFilter{Username: "bo"}, want: []string{"get_host", "login"}, total: 2},
		{name: "a part of an action, whatever its case", filter: AuditFilter{Action: "HOST"}, want: []string{"deleteHost", "get_host"}, total: 2},
		{name: "sources", filter: AuditFilter{Sources: []model.AuditSource{model.AuditMCP, model.AuditAuth}}, want: []string{"list_projects", "get_host", "login"}, total: 3},
		{name: "search a target", filter: AuditFilter{Search: "WEB-1"}, want: []string{"get_host"}, total: 1},
		{name: "search a project", filter: AuditFilter{Search: "shop"}, want: []string{"list_projects"}, total: 1},
		{name: "search the details", filter: AuditFilter{Search: `"id"`}, want: []string{"deleteHost"}, total: 1},
		{name: "an underscore is no wildcard", filter: AuditFilter{Search: "b_1"}, want: []string{"deleteHost"}, total: 1},
		{name: "a page", filter: AuditFilter{Offset: 1, Limit: 2}, want: []string{"deleteHost", "get_host"}, total: 4},
		{name: "oldest first", filter: AuditFilter{Sort: AuditSortAt}, want: []string{"login", "get_host", "deleteHost", "list_projects"}, total: 4},
		{name: "by action, whatever its case", filter: AuditFilter{Sort: AuditSortAction}, want: []string{"deleteHost", "get_host", "list_projects", "login"}, total: 4},
		{name: "by user, then newest first", filter: AuditFilter{Sort: AuditSortUsername, Desc: true}, want: []string{"get_host", "login", "list_projects", "deleteHost"}, total: 4},
		{name: "by target, the empty ones last", filter: AuditFilter{Sort: AuditSortTarget, Desc: true}, want: []string{"get_host", "deleteHost", "list_projects", "login"}, total: 4},
		{name: "an unknown column sorts by date", filter: AuditFilter{Sort: "id; drop table users", Desc: true}, want: []string{"list_projects", "deleteHost", "get_host", "login"}, total: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, total := actions(tt.filter)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.total, total)
		})
	}

	rows, _, err := repo.List(ctx, AuditFilter{Username: "bob", Sources: []model.AuditSource{model.AuditAuth}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.JSONEq(t, `{}`, string(rows[0].Details), "no details is an empty object")
	assert.Equal(t, bob.ID, *rows[0].UserID)

	names, err := repo.Usernames(ctx, "", 0)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Alice", "alice", "bob"}, names, "each username once")
	names, err = repo.Usernames(ctx, "LI", 0)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Alice", "alice"}, names, "containing the search, whatever its case")
	known, err := repo.Actions(ctx, "host", 0)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"deleteHost", "get_host"}, known)
	names, err = repo.Usernames(ctx, "", 1)
	require.NoError(t, err)
	assert.Len(t, names, 1)

	require.NoError(t, f.repos.User.Delete(ctx, bob.ID))
	rows, _, err = repo.List(ctx, AuditFilter{Username: "bob"})
	require.NoError(t, err)
	require.Len(t, rows, 2, "the events stay once the user is deleted")
	assert.Nil(t, rows[0].UserID)

	n, err := repo.PurgeBefore(ctx, day.Add(24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
	_, total := actions(AuditFilter{})
	assert.Equal(t, int64(1), total)
}
