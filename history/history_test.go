package history

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	ns  = uuid.New()
	at  = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	ptr = func(s string) *string { return &s }
)

// described prints the events: "<type> <kind> <before> → <after>".
func described(events []model.ChangeEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.EntityType+" "+e.Kind+" "+string(e.Before)+" → "+string(e.After))
	}
	return out
}

func TestRecorder(t *testing.T) {
	host, id := uuid.New(), uuid.New()
	tests := []struct {
		name   string
		record func(r *Recorder)
		want   []string
	}{
		{
			name: "added",
			record: func(r *Recorder) {
				r.Added(model.ChangeEntityService, id, &host, State{"type": "redis", "version": ptr("7.2")})
			},
			want: []string{`service added  → {"type":"redis","version":"7.2"}`},
		},
		{
			name: "removed",
			record: func(r *Recorder) {
				r.Removed(model.ChangeEntityService, id, &host, State{"type": "redis", "version": (*string)(nil)})
			},
			want: []string{`service removed {"type":"redis","version":null} → `},
		},
		{
			name: "changed, with the identity",
			record: func(r *Recorder) {
				r.Changed(model.ChangeEntityService, id, &host, State{"type": "postgresql", "version": ptr("16.4"), "port": 5432},
					State{"type": "postgresql", "version": ptr("16.6"), "port": 5432}, "type")
			},
			want: []string{`service changed {"type":"postgresql","version":"16.4"} → {"type":"postgresql","version":"16.6"}`},
		},
		{
			name: "a field set or unset",
			record: func(r *Recorder) {
				r.Changed(model.ChangeEntityRuntime, id, &host, State{"manager": nil}, State{"manager": ptr("nvm")})
			},
			want: []string{`runtime changed {"manager":null} → {"manager":"nvm"}`},
		},
		{
			name: "a JSON text and a struct holding the same",
			record: func(r *Recorder) {
				r.Changed(model.ChangeEntityHost, id, &host, State{"os": json.RawMessage(`{"version": "12", "id": "debian"}`)},
					State{"os": struct {
						ID      string `json:"id"`
						Version string `json:"version"`
					}{"debian", "12"}})
			},
		},
		{
			name: "an identity changed alone",
			record: func(r *Recorder) {
				r.Changed(model.ChangeEntityApp, id, &host, State{"path": "/srv/a", "name": "a"}, State{"path": "/srv/a", "name": "b"}, "path", "name")
			},
			want: []string{`app changed {"name":"a","path":"/srv/a"} → {"name":"b","path":"/srv/a"}`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRecorder(ns, at)
			tt.record(r)
			if tt.want == nil {
				assert.Empty(t, r.Events())
				return
			}
			assert.Equal(t, tt.want, described(r.Events()))
			for _, e := range r.Events() {
				assert.Equal(t, ns, e.NamespaceID)
				assert.Equal(t, host, *e.HostID)
				assert.Equal(t, id, *e.EntityID)
				assert.Equal(t, at, e.OccurredAt)
			}
		})
	}
}

func TestCanonical_Unmarshalable(t *testing.T) {
	assert.Nil(t, canonical(State{"f": func() {}}))
	assert.Nil(t, canonical(nil))
}

func detail(t *testing.T, d map[string]any) json.RawMessage {
	b, err := json.Marshal(d)
	require.NoError(t, err)
	return b
}

func TestLinks(t *testing.T) {
	hostA, hostB := model.Host{Base: model.Base{ID: uuid.New()}, Hostname: "a"}, model.Host{Base: model.Base{ID: uuid.New()}, Hostname: "b"}
	before := at.Add(-time.Hour)
	app := model.App{Base: model.Base{ID: uuid.New()}, HostID: hostA.ID, Name: "api", FirstSeenAt: before.Add(-time.Hour)}
	newApp := model.App{Base: model.Base{ID: uuid.New()}, HostID: hostA.ID, Name: "front", FirstSeenAt: at}
	gone := uuid.New() // an app removed since
	ws := model.Webserver{Base: model.Base{ID: uuid.New()}, HostID: hostA.ID, Type: "nginx", FirstSeenAt: before.Add(-time.Hour)}
	pg := model.Service{Base: model.Base{ID: uuid.New()}, HostID: hostB.ID, Type: "postgresql", Instance: "16/main"}
	redis := model.Service{Base: model.Base{ID: uuid.New()}, HostID: hostB.ID, Type: "redis", Instance: "default"}
	removedRedis := uuid.New()

	link := func(from uuid.UUID, fromType string, to *uuid.UUID, external, key, confidence string, computed time.Time) model.Link {
		l := model.Link{FromType: fromType, FromID: from, Source: model.LinkSourceEnv, Confidence: confidence, ComputedAt: computed,
			Detail: detail(t, map[string]any{"rule": "private_ip", "key": key, "target": "10.0.0.9:6379"})}
		if to != nil {
			l.ToType, l.ToID = model.LinkNodeService, to
		} else {
			l.ToType, l.ExternalTarget = model.LinkNodeExternal, &external
		}
		return l
	}
	inv := &model.Inventory{
		Hosts: []model.Host{hostA, hostB}, Apps: []model.App{app, newApp}, Webservers: []model.Webserver{ws},
		Services: []model.Service{pg, redis},
		Links: []model.Link{
			link(app.ID, model.LinkNodeApp, &pg.ID, "", "DATABASE_URL", model.LinkConfidenceExact, before),
			link(app.ID, model.LinkNodeApp, &removedRedis, "", "REDIS_URL", model.LinkConfidenceExact, before),
			link(app.ID, model.LinkNodeApp, nil, "10.0.0.20", "MAILER_DSN", model.LinkConfidenceExact, before),
			link(gone, model.LinkNodeApp, &pg.ID, "", "DATABASE_URL", model.LinkConfidenceExact, before),
		},
	}
	links := []model.Link{
		link(app.ID, model.LinkNodeApp, &pg.ID, "", "DATABASE_URL", model.LinkConfidenceAmbiguous, at),
		link(app.ID, model.LinkNodeApp, nil, "10.0.0.9", "REDIS_URL", model.LinkConfidenceExact, at),
		link(app.ID, model.LinkNodeApp, nil, "10.0.0.20", "MAILER_DSN", model.LinkConfidenceExact, at),
		link(newApp.ID, model.LinkNodeApp, &pg.ID, "", "DATABASE_URL", model.LinkConfidenceExact, at),
		link(ws.ID, model.LinkNodeWebserver, &redis.ID, "", "", model.LinkConfidenceExact, at),
	}

	r := NewRecorder(ns, at)
	Links(r, inv, links)
	assert.Equal(t, []string{
		`link removed {"confidence":"exact","from":"a/api","key":"REDIS_URL","reason":"","rule":"private_ip","source":"env","target":"10.0.0.9:6379","to":"10.0.0.9:6379","to_type":"service"} → `,
		`link changed {"confidence":"exact","from":"a/api","key":"DATABASE_URL","source":"env","to":"b/postgresql/16/main","to_type":"service"} → {"confidence":"ambiguous","from":"a/api","key":"DATABASE_URL","source":"env","to":"b/postgresql/16/main","to_type":"service"}`,
		`link added  → {"confidence":"exact","from":"a/api","key":"REDIS_URL","reason":"","rule":"private_ip","source":"env","target":"10.0.0.9:6379","to":"10.0.0.9","to_type":"external"}`,
		`link added  → {"confidence":"exact","from":"a/nginx","key":"","reason":"","rule":"private_ip","source":"env","target":"10.0.0.9:6379","to":"b/redis","to_type":"service"}`,
	}, described(r.Events()), "the links of the new app and the removed app are told by their own event")
	for _, e := range r.Events() {
		assert.Equal(t, hostA.ID, *e.HostID)
	}

	first := NewRecorder(ns, at)
	Links(first, &model.Inventory{Hosts: inv.Hosts, Apps: inv.Apps}, links)
	assert.Empty(t, first.Events(), "the first computation records nothing")
}
