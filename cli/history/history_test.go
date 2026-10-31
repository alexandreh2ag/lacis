package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/context"
	mockClock "github.com/alexandreh2ag/lacis/mocks/common/clock"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func withService(t *testing.T, svc service.HistoryService, err error) *bool {
	released := false
	previous := NewService
	NewService = func(*context.Context) (service.HistoryService, func(), error) {
		return svc, func() { released = true }, err
	}
	t.Cleanup(func() { NewService = previous })
	return &released
}

func run(ctx *context.Context, args ...string) (string, error) {
	cmd := GetHistoryCmd(ctx)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

var (
	now      = time.Date(2026, 10, 28, 12, 0, 0, 0, time.Local)
	hostname = "acme-pg-1"
	entityID = uuid.MustParse("01a1829e-11ad-7df4-b961-75cce10d3ea0")
)

func row(entityType, kind, before, after string) repository.ChangeEventRow {
	r := repository.ChangeEventRow{ChangeEvent: model.ChangeEvent{ID: 7, EntityType: entityType, EntityID: &entityID, Kind: kind, OccurredAt: now}, Hostname: &hostname}
	if before != "" {
		r.Before = json.RawMessage(before)
	}
	if after != "" {
		r.After = json.RawMessage(after)
	}
	return r
}

func TestHistoryCmd_Text(t *testing.T) {
	ctx := context.TestContext(nil)
	clock := mockClock.NewMockClock(gomock.NewController(t))
	clock.EXPECT().Now().Return(now)
	ctx.Clock = clock
	svc := mockService.NewMockHistoryService(gomock.NewController(t))
	noHost := row(model.ChangeEntityLink, model.ChangeAdded, "", `{"from":"h/api","to":"10.0.0.9","reason":"unknown_private_ip","confidence":"exact"}`)
	noHost.Hostname = nil
	svc.EXPECT().List(gomock.Any(), "acme", service.HistoryFilter{Hostname: hostname, EntityType: "service", Since: now.Add(-7 * 24 * time.Hour), Limit: 10}).
		Return([]repository.ChangeEventRow{
			row(model.ChangeEntityService, model.ChangeChanged, `{"type":"postgresql","instance":"16/main","version":"16.4"}`, `{"type":"postgresql","instance":"16/main","version":"16.6"}`),
			row(model.ChangeEntityService, model.ChangeRemoved, `{"type":"redis","instance":"default","version":"7.2"}`, ""),
			row(model.ChangeEntityApp, model.ChangeChanged, `{"name":"api","path":"/srv/api","code":{"version":"v1","commit":"a"},"framework":null}`,
				`{"name":"api","path":"/srv/api","code":{"version":"v2","commit":"a"},"framework":"symfony"}`),
			row(model.ChangeEntityHost, model.ChangeAdded, "", `{"hostname":"acme-pg-1"}`),
			noHost,
			row(model.ChangeEntityLink, model.ChangeRemoved, `{"from":"h/api","to":"b/redis","key":"REDIS_URL","confidence":"exact","rule":"private_ip"}`, ""),
		}, nil)
	released := withService(t, svc, nil)

	out, err := run(ctx, "-n", "acme", "--host", hostname, "--type", "service", "--since", "7d", "--limit", "10")
	require.NoError(t, err)
	assert.Equal(t, ""+
		"2026-10-28 12:00:00  acme-pg-1  service  changed  postgresql 16/main           version: 16.4 → 16.6\n"+
		"2026-10-28 12:00:00  acme-pg-1  service  removed  redis                        version: 7.2\n"+
		"2026-10-28 12:00:00  acme-pg-1  app      changed  api /srv/api                 code.version: v1 → v2, framework: ∅ → symfony\n"+
		"2026-10-28 12:00:00  acme-pg-1  host     added    acme-pg-1                    \n"+
		"2026-10-28 12:00:00  -          link     added    h/api → 10.0.0.9             confidence: exact, reason: unknown_private_ip\n"+
		"2026-10-28 12:00:00  acme-pg-1  link     removed  h/api → b/redis (REDIS_URL)  confidence: exact, rule: private_ip\n", out)
	assert.True(t, *released)
}

func TestHistoryCmd_JSON(t *testing.T) {
	svc := mockService.NewMockHistoryService(gomock.NewController(t))
	noHost := row(model.ChangeEntityHost, model.ChangeAdded, "", `{"hostname":"acme-pg-1"}`)
	noHost.Hostname, noHost.EntityID = nil, nil
	svc.EXPECT().List(gomock.Any(), "acme", service.HistoryFilter{OldestFirst: true, Limit: DefaultLimit}).
		Return([]repository.ChangeEventRow{row(model.ChangeEntityService, model.ChangeRemoved, `{"type":"redis"}`, ""), noHost}, nil)
	withService(t, svc, nil)

	out, err := run(context.TestContext(nil), "-n", "acme", "-f", "json", "--oldest-first")
	require.NoError(t, err)
	var got []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got, 2)
	assert.Equal(t, map[string]any{"id": float64(7), "occurred_at": now.Format(time.RFC3339Nano), "host": hostname, "entity_type": "service",
		"entity_id": entityID.String(), "kind": "removed", "before": map[string]any{"type": "redis"}}, got[0])
	assert.NotContains(t, got[1], "host")
	assert.NotContains(t, got[1], "entity_id")
}

func TestHistoryCmd_Errors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		svcErr  error
		listErr error
		wantErr string
	}{
		{name: "no namespace", args: []string{}, wantErr: `required flag(s) "namespace" not set`},
		{name: "unknown type", args: []string{"-n", "acme", "--type", "vm"}, wantErr: `unknown type "vm", expected one of: host, runtime, service, webserver, app, link`},
		{name: "unknown format", args: []string{"-n", "acme", "-f", "csv"}, wantErr: `unknown format "csv", expected one of: text, json`},
		{name: "invalid since", args: []string{"-n", "acme", "--since", "week"}, wantErr: "invalid --since"},
		{name: "service", args: []string{"-n", "acme"}, svcErr: errors.New("no db"), wantErr: "no db"},
		{name: "list", args: []string{"-n", "acme"}, listErr: service.ErrNamespaceNotFound, wantErr: "namespace not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mockService.NewMockHistoryService(gomock.NewController(t))
			if tt.listErr != nil {
				svc.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, tt.listErr)
			}
			withService(t, svc, tt.svcErr)
			_, err := run(context.TestContext(nil), tt.args...)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
