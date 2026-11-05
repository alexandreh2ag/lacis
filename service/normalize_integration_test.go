//go:build integration

package service

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database/dbtest"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// noQueue stands for the job queue: the test normalizes by itself.
type noQueue struct{}

func (noQueue) InsertNormalizeSnapshot(context.Context, *gorm.DB, int64) error  { return nil }
func (noQueue) InsertRecomputeLinks(context.Context, *gorm.DB, uuid.UUID) error { return nil }

type normalizeFixture struct {
	t         *testing.T
	db        *gorm.DB
	repos     *repository.Repositories
	importer  ImportService
	normalize NormalizeService
}

func newNormalizeFixture(t *testing.T) *normalizeFixture {
	db := dbtest.Open(t, "normalize")
	appCtx := appContext.TestContext(nil)
	repos := repository.NewRepositories(db)
	tx := repository.NewTransactor(db)
	snapshots := NewSnapshotService(appCtx, tx, repos.Agent, noQueue{})
	return &normalizeFixture{t: t, db: db, repos: repos, importer: NewImportService(appCtx, repos, snapshots), normalize: NewNormalizeService(appCtx, tx)}
}

// load reads a fictitious snapshot of testdata, changed by edit.
func load(t *testing.T, name string, edit func(s *snapshot.Snapshot)) json.RawMessage {
	b, err := os.ReadFile("../testdata/snapshots/" + name + ".json")
	require.NoError(t, err)
	if edit == nil {
		return b
	}
	s := &snapshot.Snapshot{}
	require.NoError(t, json.Unmarshal(b, s))
	edit(s)
	b, err = json.Marshal(s)
	require.NoError(t, err)
	return b
}

// ingest imports raw and normalizes its snapshot; it returns its host.
func (f *normalizeFixture) ingest(raw json.RawMessage) *model.Host {
	res, err := f.importer.Import(ctx, raw)
	require.NoError(f.t, err)
	ns, err := f.repos.Namespace.FindBySlug(ctx, res.Namespace)
	require.NoError(f.t, err)
	host, err := f.repos.Host.FindByHostname(ctx, ns.ID, res.Hostname)
	require.NoError(f.t, err)
	latest, err := f.repos.Snapshot.FindLatestByHost(ctx, host.ID)
	require.NoError(f.t, err)
	namespaceID, err := f.normalize.Normalize(ctx, latest.ID)
	require.NoError(f.t, err)
	assert.Equal(f.t, ns.ID, namespaceID, "the namespace whose links to recompute")
	host, err = f.repos.Host.FindByID(ctx, host.ID)
	require.NoError(f.t, err)
	return host
}

func TestNormalize_Host(t *testing.T) {
	f := newNormalizeFixture(t)
	host := f.ingest(load(t, "acme-shop-prod-1", nil))

	assert.Equal(t, "x86_64", *host.Arch)
	assert.Equal(t, "lxc", *host.Virtualization)
	assert.JSONEq(t, `{"id":"debian","version":"12","pretty":"Debian GNU/Linux 12 (bookworm)"}`, string(host.OS))
	assert.JSONEq(t, `{"cpu":{"model":"AMD EPYC 4584PX 16-Core Processor","visible_cores":4,"effective_cores":4,"source":"proc"},
		"memory_mb":{"visible":8192,"effective":8192,"source":"proc"},"disks":[{"mount":"/","size_gb":60}]}`, string(host.Resources))
	var project model.Project
	require.NoError(t, f.db.First(&project, "id = ?", host.ProjectID).Error)
	assert.Equal(t, "shop", project.Slug)
	var env model.Environment
	require.NoError(t, f.db.First(&env, "id = ?", host.EnvironmentID).Error)
	assert.Equal(t, "production", env.Slug)

	ips, err := f.repos.HostIP.FindByHost(ctx, host.ID)
	require.NoError(t, err)
	require.Len(t, ips, 1)
	assert.Equal(t, "10.0.1.21", ips[0].IP.String())
	assert.Equal(t, 24, ips[0].PrefixLen)
	assert.True(t, ips[0].DefaultRoute)
	network, err := f.repos.Network.FindByIP(ctx, host.NamespaceID, ips[0].IP.Addr)
	require.NoError(t, err)
	assert.Equal(t, "10-0-1-0-24", network.Slug, "a network created for the subnet")
	assert.Equal(t, model.NetworkKindPrivate, network.Kind)

	runtimes, err := f.repos.Runtime.FindByHost(ctx, host.ID)
	require.NoError(t, err)
	assert.Len(t, runtimes, 2)
	services, err := f.repos.Service.FindByHost(ctx, host.ID)
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "php-fpm", services[0].Type)
	assert.Equal(t, "8.3", services[0].Instance)
	endpoints, err := f.repos.Endpoint.FindByHost(ctx, host.ID)
	require.NoError(t, err)
	endpoints = slices.DeleteFunc(endpoints, func(e model.Endpoint) bool { return e.OwnerType != model.EndpointOwnerService })
	require.Len(t, endpoints, 1, "the socket of the pool, listed twice, once")
	assert.Equal(t, model.EndpointKindUnix, endpoints[0].Kind)
	assert.Equal(t, "/run/php/shop-api.sock", endpoints[0].Address)
}

func TestNormalize_Networks(t *testing.T) {
	f := newNormalizeFixture(t)
	rp := f.ingest(load(t, "acme-rp-1", nil))
	shop := f.ingest(load(t, "acme-shop-prod-2", func(s *snapshot.Snapshot) {
		s.Host.Interfaces = append(s.Host.Interfaces, snapshot.Interface{Name: "docker0", IPs: []string{"172.17.0.1/16"}})
	}))

	ips, err := f.repos.HostIP.FindByHost(ctx, shop.ID)
	require.NoError(t, err)
	assert.Len(t, ips, 2, "the IP of docker0 is kept")
	var networks []model.Network
	require.NoError(t, f.db.Where("namespace_id = ?", rp.NamespaceID).Order("slug").Find(&networks).Error)
	var slugs []string
	for _, n := range networks {
		slugs = append(slugs, n.Slug+":"+n.Kind)
	}
	assert.Equal(t, []string{"10-0-1-0-24:private", "10-0-2-0-24:private", "public:public"}, slugs,
		"shop-prod-2 joins the network the reverse proxy created; docker0 makes none")
	var cidrs []model.NetworkCIDR
	require.NoError(t, f.db.Where("namespace_id = ?", rp.NamespaceID).Order("cidr").Find(&cidrs).Error)
	assert.Len(t, cidrs, 3)
	assert.Equal(t, "203.0.113.0/27", cidrs[2].CIDR.String())
}

func TestNormalize_Removed(t *testing.T) {
	f := newNormalizeFixture(t)
	host := f.ingest(load(t, "acme-crm-prod-1", nil))
	services, err := f.repos.Service.FindByHost(ctx, host.ID)
	require.NoError(t, err)
	require.Len(t, services, 2)

	// The local Redis is gone, PHP is upgraded.
	f.ingest(load(t, "acme-crm-prod-1", func(s *snapshot.Snapshot) {
		s.Services = s.Services[:1]
		s.Services[0].Version = "8.2.30"
		s.CollectedAt = s.CollectedAt.Add(time.Hour)
	}))
	services, err = f.repos.Service.FindByHost(ctx, host.ID)
	require.NoError(t, err)
	require.Len(t, services, 2, "kept, with removed_at")
	for _, svc := range services {
		switch svc.Type {
		case "redis":
			assert.NotNil(t, svc.RemovedAt)
		case "php-fpm":
			assert.Nil(t, svc.RemovedAt)
			assert.Equal(t, "8.2.30", *svc.Version)
		}
	}
	endpoints, err := f.repos.Endpoint.FindByHost(ctx, host.ID)
	require.NoError(t, err)
	for _, e := range endpoints {
		assert.NotEqual(t, "/run/redis/redis-server.sock", e.Address, "the endpoints of a removed service go")
	}
}

func TestNormalize_Superseded(t *testing.T) {
	f := newNormalizeFixture(t)
	host := f.ingest(load(t, "acme-redis-prod-1", nil))
	first, err := f.repos.Snapshot.FindLatestByHost(ctx, host.ID)
	require.NoError(t, err)
	f.ingest(load(t, "acme-redis-prod-1", func(s *snapshot.Snapshot) {
		s.Services[0].Version = "7.4.0"
		s.CollectedAt = s.CollectedAt.Add(time.Hour)
	}))

	namespaceID, err := f.normalize.Normalize(ctx, first.ID)
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, namespaceID, "an older snapshot is skipped")
	services, err := f.repos.Service.FindByHost(ctx, host.ID)
	require.NoError(t, err)
	assert.Equal(t, "7.4.0", *services[0].Version, "the latest snapshot stays the truth")

	namespaceID, err = f.normalize.Normalize(ctx, 999999)
	assert.NoError(t, err)
	assert.Equal(t, uuid.Nil, namespaceID, "a purged snapshot is skipped")
}

func endpointsOf(t *testing.T, f *normalizeFixture, host *model.Host, kind string) []string {
	endpoints, err := f.repos.Endpoint.FindByHost(ctx, host.ID)
	require.NoError(t, err)
	var out []string
	for _, e := range endpoints {
		if e.Kind == kind && e.OwnerType == model.EndpointOwnerWebserver {
			addr := e.Address
			if e.Port != nil {
				addr += ":" + strconv.Itoa(*e.Port)
			}
			out = append(out, addr)
		}
	}
	return out
}

func TestNormalize_Webservers(t *testing.T) {
	f := newNormalizeFixture(t)
	shop := f.ingest(load(t, "acme-shop-prod-1", nil))
	webservers, err := f.repos.Webserver.FindByHost(ctx, shop.ID)
	require.NoError(t, err)
	require.Len(t, webservers, 1)
	assert.Equal(t, "nginx", webservers[0].Type)
	assert.Equal(t, "1.22.1", *webservers[0].Version)
	routing := webserverRouting{}
	require.NoError(t, json.Unmarshal(webservers[0].Routing, &routing))
	assert.Len(t, routing.Vhosts, 3)
	assert.Equal(t, []string{"api.shop.acme.example", "shop.acme.example", "www.shop.acme.example"}, endpointsOf(t, f, shop, model.EndpointKindDomain))
	assert.Equal(t, []string{":::80", "0.0.0.0:80"}, endpointsOf(t, f, shop, model.EndpointKindTCP))

	rp := f.ingest(load(t, "acme-rp-1", nil))
	domains := endpointsOf(t, f, rp, model.EndpointKindDomain)
	assert.Contains(t, domains, "api.shop.staging.acme.example")
	assert.Contains(t, domains, "acme.example")
	assert.Len(t, domains, 12, "6 domains per env, once each")

	haproxy := f.ingest(load(t, "acme-haproxy-prod-1", nil))
	assert.Equal(t, []string{"0.0.0.0:3306"}, endpointsOf(t, f, haproxy, model.EndpointKindTCP))
}

func TestNormalize_WebserverListen(t *testing.T) {
	f := newNormalizeFixture(t)
	// The Traefik entrypoints are endpoints of the webserver.
	rp := f.ingest(load(t, "acme-rp-1", func(s *snapshot.Snapshot) {
		s.Webservers[0].Listen = []string{"0.0.0.0:443", "0.0.0.0:80"}
	}))
	assert.ElementsMatch(t, []string{"0.0.0.0:443", "0.0.0.0:80"}, endpointsOf(t, f, rp, model.EndpointKindTCP))
}

func TestNormalize_Apps(t *testing.T) {
	f := newNormalizeFixture(t)
	shop := f.ingest(load(t, "acme-shop-prod-1", nil))
	apps, err := f.repos.App.FindByHost(ctx, shop.ID)
	require.NoError(t, err)
	require.Len(t, apps, 2)
	byName := map[string]model.App{}
	for _, a := range apps {
		byName[a.Name] = a
	}
	api := byName["shop-api"]
	assert.Equal(t, "web", api.Role)
	assert.Equal(t, "symfony", *api.Framework)
	assert.Equal(t, "7.2.6", *api.FrameworkVersion)
	assert.JSONEq(t, `{"type":"php","version":"8.3.14","package_version":"8.3.14-1+0~20241128.48+debian12~1.gbp3d9f8a","source":"fpm_pool"}`, string(api.Runtime))
	assert.Equal(t, model.StringArray{"api.shop.acme.example"}, api.Vhosts)
	assert.JSONEq(t, `{"APP_ENV":"prod"}`, string(api.Info))
	var launchers []snapshot.Launcher
	require.NoError(t, json.Unmarshal(api.Launchers, &launchers))
	assert.Len(t, launchers, 3)
	var env model.Environment
	require.NoError(t, f.db.First(&env, "id = ?", api.EnvironmentID).Error)
	assert.Equal(t, "production", env.Slug)

	deps, err := f.repos.AppDependency.FindByApp(ctx, api.ID)
	require.NoError(t, err)
	assert.Len(t, deps, 10)
	var mongo []string
	for _, d := range deps {
		if d.Key == "MONGODB_URL" {
			target := snapshot.Target{}
			require.NoError(t, json.Unmarshal(d.Target, &target))
			mongo = append(mongo, target.Value)
			assert.Equal(t, 27017, *d.Port)
			assert.JSONEq(t, `{"file":"/home/web/builds/shop-api/production/2-4-0/www/.env.local","line":9}`, string(d.Source))
		}
	}
	assert.ElementsMatch(t, []string{"10.0.1.70", "10.0.1.71", "10.0.1.72"}, mongo)

	// The front is gone.
	f.ingest(load(t, "acme-shop-prod-1", func(s *snapshot.Snapshot) {
		s.Apps = s.Apps[:1]
		s.Apps[0].Dependencies = s.Apps[0].Dependencies[:1]
		s.CollectedAt = s.CollectedAt.Add(time.Hour)
	}))
	apps, err = f.repos.App.FindByHost(ctx, shop.ID)
	require.NoError(t, err)
	for _, a := range apps {
		assert.Equal(t, a.Name == "shop-front", a.RemovedAt != nil, a.Name)
	}
	deps, err = f.repos.AppDependency.FindByApp(ctx, api.ID)
	require.NoError(t, err)
	assert.Len(t, deps, 1, "replaced")
}

func TestNormalize_AppMoved(t *testing.T) {
	f := newNormalizeFixture(t)
	shop := f.ingest(load(t, "acme-shop-prod-1", nil))
	apps, err := f.repos.App.FindByHost(ctx, shop.ID)
	require.NoError(t, err)
	ids := map[string]uuid.UUID{}
	for _, a := range apps {
		ids[a.Name] = a.ID
	}

	// shop-api is deployed into a new build directory; shop-front leaves
	// for a directory of another name, and its name changes.
	f.ingest(load(t, "acme-shop-prod-1", func(s *snapshot.Snapshot) {
		for i := range s.Apps {
			switch s.Apps[i].Name {
			case "shop-api":
				s.Apps[i].Path = "/home/web/builds/shop-api/production/2-5-0/www"
			case "shop-front":
				s.Apps[i].Path, s.Apps[i].Name = "/srv/front", "front"
			}
		}
		s.CollectedAt = s.CollectedAt.Add(time.Hour)
	}))
	apps, err = f.repos.App.FindByHost(ctx, shop.ID)
	require.NoError(t, err)
	byName := map[string]model.App{}
	for _, a := range apps {
		byName[a.Name] = a
	}
	require.Len(t, apps, 3, "shop-api moved, shop-front removed, front added")
	assert.Equal(t, ids["shop-api"], byName["shop-api"].ID, "the same app, in its new directory")
	assert.Equal(t, "/home/web/builds/shop-api/production/2-5-0/www", byName["shop-api"].Path)
	assert.Nil(t, byName["shop-api"].RemovedAt)
	assert.NotNil(t, byName["shop-front"].RemovedAt, "another name is another app")
	assert.NotEqual(t, ids["shop-front"], byName["front"].ID)

	var events []model.ChangeEvent
	require.NoError(t, f.db.Where("entity_id = ?", ids["shop-api"]).Find(&events).Error)
	require.Len(t, events, 1, "one change, no removal nor addition")
	assert.Equal(t, model.ChangeChanged, events[0].Kind)
}

func TestNormalize_ZendApp(t *testing.T) {
	f := newNormalizeFixture(t)
	site := f.ingest(load(t, "acme-site-prod-1", nil))
	apps, err := f.repos.App.FindByHost(ctx, site.ID)
	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.JSONEq(t, `[]`, string(apps[0].Launchers))
	deps, err := f.repos.AppDependency.FindByApp(ctx, apps[0].ID)
	require.NoError(t, err)
	require.Len(t, deps, 3)
	assert.JSONEq(t, `{"file":"/home/web/builds/site/production/www/application/configs/application.ini","line":12,"key_path":"resources.cachemanager.page.backend.options.servers"}`, string(deps[0].Source))
}

// Two apps at the same path, two services of the same type without
// instance: the first is kept, the snapshot is not lost.
func TestNormalize_Duplicates(t *testing.T) {
	f := newNormalizeFixture(t)
	shop := f.ingest(load(t, "acme-shop-prod-1", func(s *snapshot.Snapshot) {
		twin := s.Apps[1]
		twin.Path, twin.Name = s.Apps[0].Path, "twin"
		s.Apps = append(s.Apps, twin)
		s.Services = append(s.Services, s.Services[0])
		s.Webservers = append(s.Webservers, s.Webservers[0])
		s.Runtimes = append(s.Runtimes, s.Runtimes[0])
	}))
	apps, err := f.repos.App.FindByHost(ctx, shop.ID)
	require.NoError(t, err)
	require.Len(t, apps, 2)
	for _, a := range apps {
		assert.NotEqual(t, "twin", a.Name, "the first of a path is kept")
	}
}

// The addresses an app listens on are its endpoints: what targets them
// links to the app.
func TestNormalize_AppListen(t *testing.T) {
	f := newNormalizeFixture(t)
	shop := f.ingest(load(t, "acme-shop-prod-1", func(s *snapshot.Snapshot) {
		for i := range s.Apps {
			switch s.Apps[i].Name {
			case "shop-front":
				s.Apps[i].Listen = []string{"0.0.0.0:3000", "0.0.0.0:3000"}
			case "shop-api":
				s.Apps[i].Dependencies = append(s.Apps[i].Dependencies, snapshot.Dependency{Key: "FRONT_URL", Scheme: "http", Port: 3000,
					Target: &snapshot.Target{Kind: "ip", Value: "127.0.0.1"}})
			}
		}
	}))
	endpoints, err := f.repos.Endpoint.FindByHost(ctx, shop.ID)
	require.NoError(t, err)
	var owned []string
	for _, e := range endpoints {
		if e.OwnerType == model.EndpointOwnerApp {
			owned = append(owned, e.Address+":"+strconv.Itoa(*e.Port))
		}
	}
	assert.Equal(t, []string{"0.0.0.0:3000"}, owned, "once each")

	_, err = newLinkService(t, f).Recompute(ctx, shop.NamespaceID)
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-shop-prod-1/shop-front"}, linkTo(t, f, shop.NamespaceID, "acme-shop-prod-1", "shop-api")["FRONT_URL"])

	// Gone with the address.
	f.ingest(load(t, "acme-shop-prod-1", func(s *snapshot.Snapshot) { s.CollectedAt = s.CollectedAt.Add(time.Hour) }))
	endpoints, err = f.repos.Endpoint.FindByHost(ctx, shop.ID)
	require.NoError(t, err)
	for _, e := range endpoints {
		assert.NotEqual(t, model.EndpointOwnerApp, e.OwnerType)
	}
}

// Normalizations of one host running together wait for one another: each
// succeeds, and the latest snapshot is the truth.
func TestNormalize_Concurrent(t *testing.T) {
	f := newNormalizeFixture(t)
	host := f.ingest(load(t, "acme-redis-prod-1", nil))
	first, err := f.repos.Snapshot.FindLatestByHost(ctx, host.ID)
	require.NoError(t, err)
	_, err = f.importer.Import(ctx, load(t, "acme-redis-prod-1", func(s *snapshot.Snapshot) {
		s.Services[0].Version = "7.4.0"
		s.CollectedAt = s.CollectedAt.Add(time.Hour)
	}))
	require.NoError(t, err)
	latest, err := f.repos.Snapshot.FindLatestByHost(ctx, host.ID)
	require.NoError(t, err)

	errs := make(chan error, 4)
	for _, id := range []int64{latest.ID, first.ID, latest.ID, first.ID} {
		go func() {
			_, err := f.normalize.Normalize(ctx, id)
			errs <- err
		}()
	}
	for range 4 {
		assert.NoError(t, <-errs)
	}
	services, err := f.repos.Service.FindByHost(ctx, host.ID)
	require.NoError(t, err)
	assert.Equal(t, "7.4.0", *services[0].Version)
	got, err := f.repos.Host.FindByID(ctx, host.ID)
	require.NoError(t, err)
	assert.Equal(t, latest.Hash, *got.LastSnapshotHash, "the reception columns are left alone")
}
