package placement

import (
	"testing"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	corpo, website, staging, release := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	host, other := uuid.New(), uuid.New()
	labels := Place{Project: corpo, Env: staging}
	api, front, plain := uuid.New(), uuid.New(), uuid.New()
	nginx, fpm, redis, remote := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	link := func(fromType string, from uuid.UUID, toType string, to uuid.UUID) model.Link {
		return model.Link{FromType: fromType, FromID: from, ToType: toType, ToID: &to}
	}
	inv := &model.Inventory{
		Hosts: []model.Host{
			{Base: model.Base{ID: host}, ProjectID: &corpo, EnvironmentID: &staging},
			{Base: model.Base{ID: other}},
		},
		Apps: []model.App{
			// Its own project, the env of its host.
			{Base: model.Base{ID: api}, HostID: host, ProjectID: &website},
			{Base: model.Base{ID: front}, HostID: host, ProjectID: &website, EnvironmentID: &release},
			{Base: model.Base{ID: plain}, HostID: host},
		},
		Services: []model.Service{
			{Base: model.Base{ID: fpm}, HostID: host},
			{Base: model.Base{ID: redis}, HostID: host},
			{Base: model.Base{ID: remote}, HostID: other},
		},
		Webservers: []model.Webserver{{Base: model.Base{ID: nginx}, HostID: host}},
		Links: []model.Link{
			link(model.LinkNodeWebserver, nginx, model.LinkNodeApp, api),
			link(model.LinkNodeWebserver, nginx, model.LinkNodeService, fpm),
			link(model.LinkNodeApp, front, model.LinkNodeService, redis),
			// Another host: a dependency, not a place.
			link(model.LinkNodeApp, api, model.LinkNodeService, remote),
			{FromType: model.LinkNodeApp, FromID: api, ToType: model.LinkNodeExternal},
		},
	}
	p := New(inv)
	tests := []struct {
		name string
		id   uuid.UUID
		want []Place
	}{
		{"host: its labels, then its apps", host, []Place{labels, {Project: website, Env: staging}, {Project: website, Env: release}}},
		{"host without labels nor apps", other, nil},
		{"webserver: the apps it serves", nginx, []Place{labels, {Project: website, Env: staging}}},
		{"service behind a local webserver", fpm, []Place{labels, {Project: website, Env: staging}}},
		{"service an app uses", redis, []Place{labels, {Project: website, Env: release}}},
		{"service of another host", remote, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, p.Of(tt.id))
		})
	}
	assert.True(t, p.Any(host, func(pl Place) bool { return pl.Project == website }))
	assert.False(t, p.Any(other, func(Place) bool { return true }))
}

func TestOf(t *testing.T) {
	project, env := uuid.New(), uuid.New()
	host := Place{Project: uuid.New(), Env: uuid.New()}
	tests := []struct {
		name string
		app  model.App
		want Place
	}{
		{"its own", model.App{ProjectID: &project, EnvironmentID: &env}, Place{Project: project, Env: env}},
		{"its host's", model.App{}, host},
		{"mixed", model.App{ProjectID: &project}, Place{Project: project, Env: host.Env}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Of(&tt.app, host))
		})
	}
}
