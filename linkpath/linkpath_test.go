package linkpath

import (
	"encoding/json"
	"testing"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// ids names the entities of the tests.
var ids = map[string]uuid.UUID{}

func id(name string) uuid.UUID {
	if v, ok := ids[name]; ok {
		return v
	}
	v := uuid.New()
	ids[name] = v
	return v
}

func nameOfID(v uuid.UUID) string {
	for k, x := range ids {
		if x == v {
			return k
		}
	}
	return "?"
}

// link is a link of the tests: kinds are given by the prefix of the names,
// app-, svc- or ws-; ext- is an external target.
func link(from, to, confidence string, detail map[string]any) model.Link {
	kind := func(n string) string {
		switch n[:3] {
		case "app":
			return model.LinkNodeApp
		case "svc":
			return model.LinkNodeService
		case "ws-":
			return model.LinkNodeWebserver
		}
		return model.LinkNodeExternal
	}
	raw, _ := json.Marshal(detail)
	l := model.Link{FromType: kind(from), FromID: id(from), ToType: kind(to), Confidence: confidence, Detail: raw}
	if l.ToType == model.LinkNodeExternal {
		l.ExternalTarget = &to
	} else {
		v := id(to)
		l.ToID = &v
	}
	return l
}

func exact(from, to string, detail map[string]any) model.Link {
	return link(from, to, model.LinkConfidenceExact, detail)
}

// shown writes a path as the names it goes through.
func shown(paths []Path) []string {
	out := []string{}
	for _, p := range paths {
		s := nameOfID(p.First().FromID)
		for _, l := range p.Links {
			if l.ToID == nil {
				s += " > " + *l.ExternalTarget
			} else {
				s += " > " + nameOfID(*l.ToID)
			}
		}
		if p.DeadEnd {
			s += " (dead end)"
		}
		out = append(out, s)
	}
	return out
}

// inventory is a namespace: a front calling an api through a traefik then
// the nginx of the api, a mysql reached directly, an ES behind an haproxy.
func inventory() []model.Link {
	return []model.Link{
		exact("app-front", "ws-traefik", map[string]any{"key": "API_URL", "target": "https://api.example/v1"}),
		exact("app-front", "svc-mysql", map[string]any{"key": "DB", "target": "10.0.0.5:3306"}),
		exact("app-front", "ws-haproxy", map[string]any{"key": "ES", "target": "es-lb.internal:9200"}),
		exact("app-front", "ws-traefik", map[string]any{"key": "LEGACY", "target": "https://old.example"}),
		exact("app-front", "ws-traefik", map[string]any{"key": "BY_IP", "target": "10.0.0.1:80"}),
		exact("app-bo", "ws-traefik", map[string]any{"key": "SHOP", "target": "http://api.example"}),
		exact("app-other", "ws-traefik", map[string]any{"key": "BLOG", "target": "https://blog.example"}),
		exact("ws-traefik", "ws-nginx", map[string]any{"domains": []string{"api.example"}}),
		exact("ws-traefik", "app-blog", map[string]any{"domains": []string{"blog.example"}}),
		link("ws-traefik", "ext-10.9.9.9", model.LinkConfidenceInferred, map[string]any{"domains": []string{"old.example"}}),
		exact("ws-nginx", "app-api", map[string]any{"domains": []string{"*.example"}}),
		exact("app-api", "ws-nginx", map[string]any{"key": "SELF", "target": "http://127.0.0.1"}),
		exact("ws-haproxy", "svc-es1", nil),
		exact("ws-haproxy", "svc-es2", nil),
	}
}

func TestUses(t *testing.T) {
	g := New(inventory())
	assert.Equal(t, []string{
		"app-front > ws-traefik > ws-nginx > app-api",
		"app-front > svc-mysql",
		"app-front > ws-haproxy > svc-es1",
		"app-front > ws-haproxy > svc-es2",
		"app-front > ws-traefik > ext-10.9.9.9",
		"app-front > ws-traefik (dead end)",
	}, shown(g.Uses(id("app-front"))), "a call by IP to a traefik routing names is a dead end")
	assert.Equal(t, []string{"app-api > ws-nginx > app-api"}, shown(g.Uses(id("app-api"))),
		"a call by IP to a webserver whose routes all lead to one place goes there")
	assert.Empty(t, g.Uses(id("ws-unknown")))
}

func TestUsedBy(t *testing.T) {
	g := New(inventory())
	assert.Equal(t, []string{
		"app-api > ws-nginx > app-api",
		"app-front > ws-traefik > ws-nginx > app-api",
		"app-bo > ws-traefik > ws-nginx > app-api",
	}, shown(g.UsedBy(id("app-api"))), "the nearest consumers first; blog.example does not lead to the api")
	assert.Equal(t, []string{"app-other > ws-traefik > app-blog"}, shown(g.UsedBy(id("app-blog"))))
	assert.Equal(t, []string{"app-front > ws-haproxy > svc-es1"}, shown(g.UsedBy(id("svc-es1"))))
	assert.Equal(t, []string{"app-front > ws-traefik (dead end)"}, shown(g.UsedBy(id("ws-traefik"))),
		"a webserver is used only where the path ends at it")
	assert.Empty(t, g.UsedBy(id("ws-nginx")))
}

func TestRoutes(t *testing.T) {
	tests := []struct {
		name  string
		links []model.Link
		from  string
		want  []string
	}{
		{
			name: "the route of the name wins over the default one",
			links: []model.Link{
				exact("app-a", "ws-x", map[string]any{"target": "https://shop.example"}),
				exact("ws-x", "app-shop", map[string]any{"domains": []string{"shop.example"}}),
				exact("ws-x", "app-default", nil),
			},
			from: "app-a",
			want: []string{"app-a > ws-x > app-shop"},
		},
		{
			name: "a name no route serves takes the default route",
			links: []model.Link{
				exact("app-a", "ws-x", map[string]any{"target": "https://other.example"}),
				exact("ws-x", "app-shop", map[string]any{"domains": []string{"shop.example"}}),
				exact("ws-x", "app-default", nil),
			},
			from: "app-a",
			want: []string{"app-a > ws-x > app-default"},
		},
		{
			name: "a call by IP takes the routes of no name",
			links: []model.Link{
				exact("app-a", "ws-x", map[string]any{"target": "10.0.0.1"}),
				exact("ws-x", "app-shop", map[string]any{"domains": []string{"shop.example"}}),
				exact("ws-x", "app-default", nil),
			},
			from: "app-a",
			want: []string{"app-a > ws-x > app-default"},
		},
		{
			name: "a call by IP to routes of names leading to several places is a dead end",
			links: []model.Link{
				exact("app-a", "ws-x", map[string]any{"target": "10.0.0.1"}),
				exact("ws-x", "app-shop", map[string]any{"domains": []string{"shop.example"}}),
				exact("ws-x", "ext-s3", map[string]any{"domains": []string{"media.example"}}),
			},
			from: "app-a",
			want: []string{"app-a > ws-x (dead end)"},
		},
		{
			name: "a call by IP to routes of names leading to one external target goes there, once",
			links: []model.Link{
				exact("app-a", "ws-x", map[string]any{"target": "10.0.0.1"}),
				exact("ws-x", "ext-s3", map[string]any{"domains": []string{"media.example"}}),
				exact("ws-x", "ext-s3", map[string]any{"domains": []string{"cdn.example"}}),
			},
			from: "app-a",
			want: []string{"app-a > ws-x > ext-s3"},
		},
		{
			name: "of two routes to the same end, the shortest",
			links: []model.Link{
				exact("app-a", "ws-x", map[string]any{"target": "https://www.example"}),
				exact("ws-x", "ext-webflow", map[string]any{"domains": []string{"www.example"}}),
				exact("ws-x", "ws-y", map[string]any{"domains": []string{"www.example"}}),
				exact("ws-y", "ext-webflow", nil),
			},
			from: "app-a",
			want: []string{"app-a > ws-x > ext-webflow"},
		},
		{
			name: "the php-fpm running the app a vhost serves is not a target",
			links: []model.Link{
				exact("app-a", "ws-x", map[string]any{"target": "https://api.example"}),
				exact("ws-x", "app-api", map[string]any{"domains": []string{"api.example"}}),
				exact("ws-x", "svc-fpm", map[string]any{"domains": []string{"api.example"}}),
				exact("ws-x", "ext-s3", map[string]any{"domains": []string{"api.example"}}),
			},
			from: "app-a",
			want: []string{"app-a > ws-x > app-api", "app-a > ws-x > ext-s3"},
		},
		{
			name: "a service routed alone is a target",
			links: []model.Link{
				exact("app-a", "ws-x", map[string]any{"target": "es.example"}),
				exact("ws-x", "svc-es", map[string]any{"domains": []string{"es.example"}}),
			},
			from: "app-a",
			want: []string{"app-a > ws-x > svc-es"},
		},
		{
			name: "a loop of webservers stops",
			links: []model.Link{
				exact("app-a", "ws-x", map[string]any{"target": "loop.example"}),
				exact("ws-x", "ws-y", nil),
				exact("ws-y", "ws-x", nil),
			},
			from: "app-a",
			want: []string{"app-a > ws-x > ws-y > ws-x (dead end)"},
		},
		{
			name: "a webserver without route is a dead end",
			links: []model.Link{
				exact("app-a", "ws-x", map[string]any{"target": "x.example"}),
			},
			from: "app-a",
			want: []string{"app-a > ws-x (dead end)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shown(New(tt.links).Uses(id(tt.from))))
		})
	}
}

func TestPath(t *testing.T) {
	links := []model.Link{
		link("app-a", "ws-x", model.LinkConfidenceInferred, nil),
		link("ws-x", "app-b", model.LinkConfidenceExact, nil),
	}
	p := Path{Links: []*model.Link{&links[0], &links[1]}}
	assert.Equal(t, model.LinkConfidenceInferred, p.Confidence())
	assert.Same(t, &links[0], p.First())
	assert.Same(t, &links[1], p.Last())
	links[1].Confidence = model.LinkConfidenceAmbiguous
	assert.Equal(t, model.LinkConfidenceAmbiguous, p.Confidence())
	links[0].Confidence, links[1].Confidence = model.LinkConfidenceExact, model.LinkConfidenceExact
	assert.Equal(t, model.LinkConfidenceExact, p.Confidence())
}
