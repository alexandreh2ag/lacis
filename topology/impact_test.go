package topology

import (
	"encoding/json"
	"testing"

	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// proxied: a traefik of production routes prod.example to the site of
// production, which uses the database, and stg.example to the site of
// staging. A staging worker calls stg.example, a production one
// prod.example, through that traefik.
func proxied(t *testing.T) (*model.Inventory, map[string]uuid.UUID) {
	id := map[string]uuid.UUID{}
	for _, k := range []string{"rp", "webProd", "webStg", "db", "workerStg", "workerProd", "traefik", "nginxProd", "nginxStg", "site", "siteStg", "mariadb", "jobStg", "jobProd"} {
		id[k] = uuid.New()
	}
	link := func(fromType string, from uuid.UUID, toType string, to uuid.UUID, d linker.Detail) model.Link {
		b, err := json.Marshal(d)
		require.NoError(t, err)
		return model.Link{FromType: fromType, FromID: from, ToType: toType, ToID: &to, Source: model.LinkSourceEnv, Confidence: model.LinkConfidenceExact, Detail: b}
	}
	host := func(k string) model.Host { return model.Host{Base: model.Base{ID: id[k]}, Hostname: k} }
	app := func(k, h string) model.App { return model.App{Base: model.Base{ID: id[k]}, HostID: id[h], Name: k} }
	inv := &model.Inventory{
		Hosts: []model.Host{host("rp"), host("webProd"), host("webStg"), host("db"), host("workerStg"), host("workerProd")},
		Apps:  []model.App{app("site", "webProd"), app("siteStg", "webStg"), app("jobStg", "workerStg"), app("jobProd", "workerProd")},
		Webservers: []model.Webserver{{Base: model.Base{ID: id["traefik"]}, HostID: id["rp"], Type: "traefik"},
			{Base: model.Base{ID: id["nginxProd"]}, HostID: id["webProd"], Type: "nginx"}, {Base: model.Base{ID: id["nginxStg"]}, HostID: id["webStg"], Type: "nginx"}},
		Services: []model.Service{{Base: model.Base{ID: id["mariadb"]}, HostID: id["db"], Type: "mariadb"}},
		Links: []model.Link{
			link(model.LinkNodeWebserver, id["traefik"], model.LinkNodeWebserver, id["nginxProd"], linker.Detail{Domains: []string{"prod.example"}}),
			link(model.LinkNodeWebserver, id["traefik"], model.LinkNodeWebserver, id["nginxStg"], linker.Detail{Domains: []string{"*.stg.example"}}),
			link(model.LinkNodeWebserver, id["nginxProd"], model.LinkNodeApp, id["site"], linker.Detail{Domains: []string{"prod.example"}}),
			link(model.LinkNodeWebserver, id["nginxStg"], model.LinkNodeApp, id["siteStg"], linker.Detail{Domains: []string{"api.stg.example"}}),
			link(model.LinkNodeApp, id["site"], model.LinkNodeService, id["mariadb"], linker.Detail{Key: "DATABASE_URL"}),
			link(model.LinkNodeApp, id["jobStg"], model.LinkNodeWebserver, id["traefik"], linker.Detail{Key: "API_URL", Target: "https://api.stg.example/v1"}),
			link(model.LinkNodeApp, id["jobProd"], model.LinkNodeWebserver, id["traefik"], linker.Detail{Key: "SITE_URL", Target: "prod.example:443"}),
		},
	}
	return inv, id
}

func hostNode(id map[string]uuid.UUID, k string) string { return "host:" + id[k].String() }

func TestBuildImpact_Dependents(t *testing.T) {
	inv, id := proxied(t)
	got, err := BuildImpact(inv, Filter{Level: LevelHost}, hostNode(id, "db"), Dependents)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{hostNode(id, "webProd"): 1, hostNode(id, "rp"): 3, hostNode(id, "workerProd"): 4}, got.Levels,
		"the staging worker calls traefik for a staging domain: the database of production is not its business")
	assert.ElementsMatch(t, []string{
		hostNode(id, "rp") + "→" + hostNode(id, "webProd"),
		hostNode(id, "webProd") + "→" + hostNode(id, "db"),
		hostNode(id, "workerProd") + "→" + hostNode(id, "rp"),
	}, got.Edges)
}

func TestBuildImpact_Needs(t *testing.T) {
	inv, id := proxied(t)
	got, err := BuildImpact(inv, Filter{Level: LevelHost}, hostNode(id, "workerStg"), Needs)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{hostNode(id, "rp"): 1, hostNode(id, "webStg"): 2}, got.Levels,
		"traefik is only needed for api.stg.example, routed to staging")

	got, err = BuildImpact(inv, Filter{Level: LevelHost}, hostNode(id, "workerProd"), Needs)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{hostNode(id, "rp"): 1, hostNode(id, "webProd"): 2, hostNode(id, "db"): 4}, got.Levels)
}

func TestBuildImpact_OpenedZone(t *testing.T) {
	inv, id := proxied(t)
	zone := hostNode(id, "db")
	got, err := BuildImpact(inv, Filter{Level: LevelHost, Expand: []string{zone}}, zone, Dependents)
	require.NoError(t, err)
	assert.Contains(t, got.Levels, hostNode(id, "webProd"), "an opened zone stands for its content")
	assert.NotContains(t, got.Levels, "item:"+zone+"|service|mariadb")
}

func TestNameOf(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.stg.example/v1": "api.stg.example",
		"prod.example:443":           "prod.example",
		"redis.local":                "redis.local",
		"10.0.1.5:6379":              "",
		"[::1]:80":                   "",
		"/run/php/fpm.sock":          "",
		"":                           "",
	} {
		assert.Equal(t, want, nameOf(in), in)
	}
}
