package apps

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/fakefs"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// host is a fake machine: files, links, processes and listening sockets.
type host struct {
	t     *testing.T
	fs    *fakefs.FS
	tcp   []string
	inode int
}

func newHost(t *testing.T) *host {
	return &host{t: t, fs: fakefs.New(), inode: 5000}
}

func (h *host) process(pid int, exe, cwd string, cmdline ...string) {
	h.child(pid, 1, exe, cwd, cmdline...)
}

func (h *host) child(pid, ppid int, exe, cwd string, cmdline ...string) {
	dir := fmt.Sprintf("/proc/%d/", pid)
	h.fs.Write(h.t, dir+"status", fmt.Sprintf("Name:\tx\nPPid:\t%d\nUid:\t1000\t1000\t1000\t1000\n", ppid))
	h.fs.Write(h.t, dir+"cmdline", strings.Join(cmdline, "\x00")+"\x00")
	h.fs.Link(dir+"exe", exe)
	h.fs.Link(dir+"cwd", cwd)
}

func (h *host) listen(pid int, addr string) {
	h.inode++
	fd := fmt.Sprintf("/proc/%d/fd/%d", pid, h.inode)
	h.fs.Write(h.t, fd, "")
	h.fs.Link(fd, fmt.Sprintf("socket:[%d]", h.inode))
	ap := netip.MustParseAddrPort(addr)
	b := ap.Addr().As4()
	h.tcp = append(h.tcp, fmt.Sprintf("0: %08X:%04X 00000000:0000 0A 0:0 0:0 0 0 0 %d", binary.LittleEndian.Uint32(b[:]), ap.Port(), h.inode))
}

func (h *host) collect(out *snapshot.Snapshot, scan ...string) error {
	h.fs.Write(h.t, system.TCPFile, "header\n"+strings.Join(h.tcp, "\n")+"\n")
	h.fs.Write(h.t, system.UnixFile, "header\n")
	ctx := context.TestContext(nil)
	ctx.Fs = h.fs
	ctx.Config.Labels = labels.Labels{Namespace: "acme", Project: "client", Env: "staging"}
	ctx.Config.Collectors.Apps.ScanPaths = scan
	ctx.Config.Collectors.Apps.Exclude = config.DefaultConfig().Collectors.Apps.Exclude
	sys, err := system.Load(ctx)
	require.NoError(h.t, err)
	c := &Collector{}
	require.True(h.t, c.Applicable(ctx, sys))
	return c.Collect(h.t.Context(), ctx, sys, out)
}

const composerLock = `{"packages": [{"name": "symfony/console", "version": "v6.4.12"}, {"name": "symfony/framework-bundle", "version": "v6.4.12"}]}`

func TestCollect(t *testing.T) {
	h := newHost(t)
	h.fs.Files(t, map[string]string{
		// Symfony API, deployed with a current link, behind nginx and FPM.
		"/var/www/client/releases/42/composer.json":    "{}",
		"/var/www/client/releases/42/composer.lock":    composerLock,
		"/var/www/client/releases/42/public/index.php": "",
		"/var/www/client/releases/42/.app.lacis.json": `{"app": "api", "version": "v2.3.1", "commit": "a1b2c3d", "branch": "main",
			"repository": "https://ci:secret@gitlab.example.com/client/api.git", "deployed_at": "2026-10-21T16:02:00Z"}`,
		// A worker of its own, PHP through a link.
		"/var/www/worker/composer.json": "{}",
		"/usr/bin/php":                  "",
		"/etc/supervisor/conf.d/w.conf": "[program:messenger]\ncommand=php bin/console messenger:consume\ndirectory=/var/www/worker\nuser=deploy\n" +
			// Without directory, the script path of the command gives the app.
			"[program:sync]\ncommand=/usr/bin/php8.2 /var/www/client/current/bin/console app:sync\n" +
			"[program:lost]\ncommand=/usr/bin/php8.2 -r 'sleep(1);'\n",
		"/etc/supervisor/conf.d/front.conf": "[program:front-start]\ncommand=bash -c \"npm start\"\ndirectory=/home/deploy/front\n" +
			"[program:front-gtask]\ncommand=node gtask.js\ndirectory=/home/deploy/front\n",
		// Next.js front behind a proxy_pass to its node process.
		"/home/deploy/front/package.json":                   `{"dependencies": {"next": "^14.2.0"}}`,
		"/home/deploy/front/node_modules/next/package.json": `{"version": "14.2.3"}`,
		"/home/deploy/front/.git/HEAD":                      "ref: refs/heads/main\n",
		"/home/deploy/front/.git/refs/heads/main":           "c0ffee00\n",
		"/home/deploy/front/.git/packed-refs":               "# pack-refs with: peeled\nbeef0000 refs/tags/v1.3.0\nfeed0000 refs/tags/v1.4.0\n^c0ffee00\n",
		"/home/deploy/front/.git/config":                    "[core]\n\tbare = false\n[remote \"origin\"]\n\turl = https://deploy:glpat-xyz@gitlab.example.com/acme/front.git\n",
		// Apache site on FPM through localhost:9000, with both version files.
		"/home/web/var/www/siteweb/staging/www/composer.json":    "{}",
		"/home/web/var/www/siteweb/staging/www/.app.lacis.json":  `{"app": "siteweb", "project": "talents", "version": "1.0.0", "commit": "abc"}`,
		"/home/web/var/www/siteweb/staging/www/.app.lacis.yaml":  "app: ignored\n",
		"/home/web/var/www/siteweb/staging/www/public/index.php": "",
		// Scan paths: a Zend 1 app, a deployment, a tool with .nvmrc, excluded dirs.
		"/srv/apps/legacy/application/configs/application.ini": "[production]\n",
		"/srv/apps/legacy/library/Zend/Version.php":            "<?php\nclass Zend_Version {\n    const VERSION = '1.12.20';\n}\n",
		"/srv/apps/legacy/.git/HEAD":                           "0123456789abcdef\n",
		"/srv/apps/deploy/releases/2/composer.json":            "{}",
		"/srv/apps/deploy/releases/3/composer.json":            "{}",
		"/srv/apps/tool/package.json":                          "{}",
		"/srv/apps/shop/production/www/composer.json":          "{}",
		"/srv/apps/shop/pp/www/composer.json":                  "{}",
		"/srv/apps/shop/preview/www/composer.json":             "{}",
		"/srv/apps/shop/preview/www/.app.lacis.yml":            "env: review\nversion: v1\ncommit: abc\n",
		"/srv/apps/tool/.nvmrc":                                "v20\n",
		"/srv/apps/vendor/x/composer.json":                     "{}",
		"/srv/apps/.cache/y/composer.json":                     "{}",
		// A dotfiles repository in a home directory is not an app.
		"/home/deploy/.git/HEAD":                                    "ref: refs/heads/main\n",
		"/home/deploy/.cache/tool/1.0/package.json":                 "{}",
		"/var/www/client/releases/42/node_modules/pkg/package.json": "{}",
	})
	h.fs.Link("/var/www/client/current", "releases/42")
	h.fs.Link("/srv/apps/deploy/current", "releases/3")
	h.fs.Link("/usr/bin/php", "/usr/bin/php8.2")
	// The front runs under supervisor: its server through bash, and gtask.
	h.process(1100, "/usr/bin/python3.11", "/", "/usr/bin/python3", "/usr/bin/supervisord", "-n")
	h.child(1230, 1100, "/usr/bin/bash", "/home/deploy/front", "bash", "-c", "npm start")
	h.child(1234, 1230, "/home/deploy/.nvm/versions/node/v20.11.1/bin/node", "/home/deploy/front", "node", "server.js")
	h.child(1240, 1100, "/home/deploy/.nvm/versions/node/v20.11.1/bin/node", "/home/deploy/front", "node", "gtask.js")
	h.listen(1234, "127.0.0.1:3000")
	h.process(1300, "/usr/bin/bash", "/home/deploy", "bash")
	h.process(1400, "/usr/bin/php8.2", "/var/www/client/releases/42", "php", "bin/console", "app:sync")
	// Tools of a hidden or excluded directory are not apps.
	h.process(1500, "/usr/bin/node", "/home/deploy/.cache/tool/1.0", "node")
	h.process(1501, "/usr/bin/node", "/var/www/client/releases/42/node_modules/pkg", "node")

	out := &snapshot.Snapshot{
		Runtimes: []snapshot.Runtime{
			{Type: "node", Version: "20.9.0", Path: "/home/deploy/.nvm/versions/node/v20.9.0/bin/node", Manager: "nvm"},
			{Type: "node", Version: "20.11.1", Path: "/home/deploy/.nvm/versions/node/v20.11.1/bin/node", Manager: "nvm"},
			{Type: "php", Version: "8.2.24", Path: "/usr/bin/php8.2"},
		},
		Services: []snapshot.Service{
			{Type: "php-fpm", Version: "8.2.24", Pools: []snapshot.FPMPool{{Name: "client", Listen: "unix:/run/php/client.sock"}}},
			{Type: "php-fpm", Version: "7.0.33", Pools: []snapshot.FPMPool{{Name: "www", Listen: "127.0.0.1:9000"}}},
		},
		Webservers: []snapshot.Webserver{
			{
				Type: "nginx",
				Vhosts: []snapshot.Vhost{
					{ServerNames: []string{"staging.client.fr", "api.staging.client.fr"}, Root: "/var/www/client/current/public", Upstreams: []string{"clientfpm"}},
					{ServerNames: []string{"front.client.fr"}, Upstreams: []string{"clientfpm", "http://127.0.0.1:3000", "http://127.0.0.1:9999"}},
					{ServerNames: []string{"static.client.fr"}, Root: "/var/www/html"},
				},
				Upstreams: []snapshot.Upstream{{Name: "clientfpm", Servers: []string{"unix:/run/php/client.sock"}}},
			},
			{Type: "apache", Vhosts: []snapshot.Vhost{
				{ServerNames: []string{"staging.talentspros.com"}, Root: "/home/web/var/www/siteweb/staging/www/public", Upstreams: []string{"localhost:9000"}},
			}},
		},
	}
	err := h.collect(out, "/srv/apps", "/nowhere")
	assert.EqualError(t, err, "/home/web/var/www/siteweb/staging/www: several version files (.app.lacis.json, .app.lacis.yaml), .app.lacis.json used")

	deployedAt := time.Date(2026, 10, 21, 16, 2, 0, 0, time.UTC)
	want := []snapshot.App{
		{
			Path: "/home/deploy/front", Name: "front", Role: "web", Project: "client", Env: "staging",
			Framework: &snapshot.Framework{Name: "next", Version: "14.2.3"},
			Runtime:   &snapshot.AppRuntime{Type: "node", Version: "20.11.1", Path: "/home/deploy/.nvm/versions/node/v20.11.1/bin/node", Source: "process"},
			Code:      &snapshot.Code{Source: "git", Commit: "c0ffee00", Branch: "main", Tag: "v1.4.0", Version: "v1.4.0", Repository: "https://gitlab.example.com/acme/front.git"},
			Vhosts:    []string{"front.client.fr"},
			Launchers: []snapshot.Launcher{
				{Type: "supervisor", Name: "front-gtask", Role: "worker", Command: "node gtask.js", Directory: "/home/deploy/front"},
				{Type: "supervisor", Name: "front-start", Role: "web", Command: `bash -c "npm start"`, Directory: "/home/deploy/front"},
			},
			Dependencies: []snapshot.Dependency{},
		},
		{
			Path: "/home/web/var/www/siteweb/staging/www", Name: "siteweb", Role: "web", Project: "talents", Env: "staging",
			Runtime:      &snapshot.AppRuntime{Type: "php", Version: "7.0.33", Source: "fpm_pool"},
			Code:         &snapshot.Code{Source: "deploy_info", Version: "1.0.0", Commit: "abc"},
			Vhosts:       []string{"staging.talentspros.com"},
			Launchers:    []snapshot.Launcher{},
			Dependencies: []snapshot.Dependency{},
		},
		{
			Path: "/srv/apps/deploy/current", ResolvedPath: "/srv/apps/deploy/releases/3", Name: "deploy", Role: "worker", Project: "client", Env: "staging",
			Launchers:    []snapshot.Launcher{},
			Dependencies: []snapshot.Dependency{},
		},
		{
			Path: "/srv/apps/legacy", Name: "legacy", Role: "worker", Project: "client", Env: "staging",
			Framework:    &snapshot.Framework{Name: "zend1", Version: "1.12.20"},
			Code:         &snapshot.Code{Source: "git", Commit: "0123456789abcdef"},
			Launchers:    []snapshot.Launcher{},
			Dependencies: []snapshot.Dependency{},
		},
		{
			Path: "/srv/apps/shop/pp/www", Name: "shop", Role: "worker", Project: "client", Env: "staging",
			Launchers:    []snapshot.Launcher{},
			Dependencies: []snapshot.Dependency{},
		},
		{
			Path: "/srv/apps/shop/preview/www", Name: "shop", Role: "worker", Project: "client", Env: "review",
			Code:         &snapshot.Code{Source: "deploy_info", Version: "v1", Commit: "abc"},
			Launchers:    []snapshot.Launcher{},
			Dependencies: []snapshot.Dependency{},
		},
		{
			Path: "/srv/apps/shop/production/www", Name: "shop", Role: "worker", Project: "client", Env: "production",
			Launchers:    []snapshot.Launcher{},
			Dependencies: []snapshot.Dependency{},
		},
		{
			Path: "/srv/apps/tool", Name: "tool", Role: "worker", Project: "client", Env: "staging",
			Runtime:      &snapshot.AppRuntime{Type: "node", Version: "20.11.1", Path: "/home/deploy/.nvm/versions/node/v20.11.1/bin/node", Source: "nvmrc"},
			Launchers:    []snapshot.Launcher{},
			Dependencies: []snapshot.Dependency{},
		},
		{
			Path: "/var/www/client/current", ResolvedPath: "/var/www/client/releases/42", Name: "api", Role: "web", Project: "client", Env: "staging",
			Framework: &snapshot.Framework{Name: "symfony", Version: "6.4.12"},
			Runtime:   &snapshot.AppRuntime{Type: "php", Version: "8.2.24", Source: "fpm_pool"},
			Code: &snapshot.Code{Source: "deploy_info", Version: "v2.3.1", Commit: "a1b2c3d", Branch: "main",
				Repository: "https://gitlab.example.com/client/api.git", DeployedAt: &deployedAt},
			Vhosts:       []string{"api.staging.client.fr", "staging.client.fr"},
			Launchers:    []snapshot.Launcher{{Type: "supervisor", Name: "sync", Role: "worker", Command: "/usr/bin/php8.2 /var/www/client/current/bin/console app:sync"}},
			Dependencies: []snapshot.Dependency{},
		},
		{
			Path: "/var/www/worker", Name: "worker", Role: "worker", Project: "client", Env: "staging",
			Runtime:      &snapshot.AppRuntime{Type: "php", Version: "8.2.24", Path: "/usr/bin/php8.2", Source: "launcher"},
			Launchers:    []snapshot.Launcher{{Type: "supervisor", Name: "messenger", Role: "worker", Command: "php bin/console messenger:consume", Directory: "/var/www/worker", User: "deploy"}},
			Dependencies: []snapshot.Dependency{},
		},
	}
	assert.Equal(t, want, out.Apps)
}

func TestNormalizeAddr(t *testing.T) {
	for in, want := range map[string]string{
		"localhost:9000":        "127.0.0.1:9000",
		"fcgi://127.0.0.1:9000": "127.0.0.1:9000",
		"[::1]:9000":            "127.0.0.1:9000",
		"127.0.1.1:80":          "127.0.0.1:80",
		"http://10.0.0.1:3000/": "10.0.0.1:3000",
		"unix:/run/php/x.sock":  "unix:/run/php/x.sock",
		"https://127.0.0.1":     "127.0.0.1",
		"[::]:80":               "[::]:80",
	} {
		assert.Equal(t, want, normalizeAddr(in), in)
	}
}

func TestNames(t *testing.T) {
	assert.Equal(t, "client", dirName("/var/www/client/current"))
	assert.Equal(t, "client", dirName("/var/www/client/releases/42"))
	assert.Equal(t, "siteweb", dirName("/home/web/var/www/siteweb/staging/www"))
	assert.Equal(t, "api-catalog", dirName("/home/svc/var/www/espace.fr/api-catalog/production/www"))
	assert.Equal(t, "simulateur", dirName("/home/svc/builds/simulateur/production/1-1-1/www"))
	assert.Equal(t, "app", dirName("/srv/www/app"), "all generic: the base")
	assert.Equal(t, "html", dirName("/var/www/html"))
	assert.Equal(t, "api", dirName("/home/web/var/www/ecom/api/dev1/www"), "numbered environments skipped")
	assert.Equal(t, "admin", dirName("/home/web/var/www/ecom/admin/staging2/www"))
	assert.Equal(t, "www", dirName("/home/web/www"))
	assert.Equal(t, "api", repoName("git@gitlab.example.com:client/api.git"))
	assert.Equal(t, "api", repoName("https://github.com/client/api/"))
	assert.Equal(t, "git@gitlab.example.com:client/api.git", stripCredentials("git@gitlab.example.com:client/api.git"))
	assert.Equal(t, "https://gitlab.example.com/x.git", stripCredentials("https://u:p@gitlab.example.com/x.git"))
	assert.True(t, tooHigh("/home/deploy"))
	assert.True(t, tooHigh("/srv"))
	assert.False(t, tooHigh("/srv/app"))
}

func TestGitRepo_Code(t *testing.T) {
	tests := []struct {
		name    string
		repo    gitRepo
		version string
	}{
		{name: "tag", repo: gitRepo{commit: "aaa", branch: "main", tag: "v2"}, version: "v2"},
		{name: "branch without tag", repo: gitRepo{commit: "aaa", branch: "staging"}, version: "staging"},
		{name: "detached HEAD", repo: gitRepo{commit: "aaa"}, version: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.repo.code()
			assert.Equal(t, tt.version, got.Version)
			assert.Equal(t, snapshot.CodeSource("git"), got.Source)
			assert.Equal(t, tt.repo.commit, got.Commit)
			assert.Equal(t, tt.repo.branch, got.Branch)
			assert.Equal(t, tt.repo.tag, got.Tag)
		})
	}
}

func TestReadGit(t *testing.T) {
	f := fakefs.New()
	f.Files(t, map[string]string{
		"/repos/wt/.git":                     "gitdir: ../main/.git/worktrees/wt\n",
		"/repos/main/.git/worktrees/wt/HEAD": "ref: refs/heads/feature\n",
		"/repos/bad/.git":                    "garbage\n",
		"/repos/nohead/.git/config":          "",
		"/repos/loose/.git/HEAD":             "ref: refs/heads/main\n",
		"/repos/loose/.git/refs/heads/main":  "aaa\n",
		"/repos/loose/.git/refs/tags/v2":     "aaa\n",
	})
	repo, err := readGit(f, "/repos/wt")
	require.NoError(t, err)
	assert.Equal(t, &gitRepo{branch: "feature"}, repo, "branch without commit yet")

	_, err = readGit(f, "/repos/bad")
	assert.ErrorContains(t, err, "not a gitdir file")
	_, err = readGit(f, "/repos/nohead")
	assert.ErrorContains(t, err, "read /repos/nohead/.git/HEAD")

	repo, err = readGit(f, "/repos/loose")
	require.NoError(t, err)
	assert.Equal(t, &gitRepo{commit: "aaa", branch: "main", tag: "v2"}, repo)

	repo, err = readGit(f, "/repos/none")
	require.NoError(t, err)
	assert.Nil(t, repo)
}

func TestReadDeployInfo(t *testing.T) {
	f := fakefs.New()
	f.Files(t, map[string]string{
		"/a/.app.lacis.yaml": "app: api\nversion: v1\ncommit: abc\ndeployed_at: 2026-10-21T16:02:00Z\n",
		"/b/.app.lacis.json": "{",
		"/c/.app.lacis.yaml": "app: [",
		"/d/.app.lacis.json": `{"app": "x"}`,
	})
	info, warnings := readDeployInfo(f, "/a")
	require.Empty(t, warnings)
	assert.Equal(t, "api", info.App)
	assert.Equal(t, 2026, info.DeployedAt.Year())

	_, warnings = readDeployInfo(f, "/b")
	assert.ErrorContains(t, warnings[0], "/b/.app.lacis.json")
	_, warnings = readDeployInfo(f, "/c")
	assert.ErrorContains(t, warnings[0], "/c/.app.lacis.yaml")
	info, warnings = readDeployInfo(f, "/d")
	assert.Equal(t, "x", info.App)
	assert.ErrorContains(t, warnings[0], "without version or commit")
}

func TestFrameworks(t *testing.T) {
	f := fakefs.New()
	f.Files(t, map[string]string{
		"/laminas/composer.lock":                         `{"packages": [{"name": "laminas/laminas-mvc", "version": "3.7.0"}]}`,
		"/zend/composer.lock":                            `{"packages": [{"name": "zendframework/zend-mvc", "version": "2.7.15"}]}`,
		"/next-lock/package.json":                        `{"devDependencies": {"next": "^13"}}`,
		"/next-lock/package-lock.json":                   `{"packages": {"node_modules/next": {"version": "13.5.6"}}}`,
		"/next-range/package.json":                       `{"dependencies": {"next": "^12.0.0"}}`,
		"/zend1-ini/application/configs/application.ini": "",
		"/laravel/composer.lock":                         `{"packages": [{"name": "laravel/framework", "version": "v11.9.0"}]}`,
		"/nuxt/package.json":                             `{"dependencies": {"nuxt": "3.12.0"}}`,
		"/custom/artisan":                                "",
		"/plain/package.json":                            `{"dependencies": {"express": "4"}}`,
	})
	declared := []config.FrameworkDetector{
		{Name: "laravel", Detect: config.FrameworkDetect{ComposerPackage: "laravel/framework"}},
		{Name: "nuxt", Detect: config.FrameworkDetect{NpmPackage: "nuxt"}},
		{Name: "custom", Detect: config.FrameworkDetect{File: "artisan"}},
		{Name: "symfony", Detect: config.FrameworkDetect{File: "symfony.lock"}},
		{Name: "empty"},
	}
	all := detectors(declared)
	tests := map[string]*snapshot.Framework{
		"/laminas":    {Name: "laminas", Version: "3.7.0"},
		"/zend":       {Name: "zend", Version: "2.7.15"},
		"/next-lock":  {Name: "next", Version: "13.5.6"},
		"/next-range": {Name: "next", Version: "^12.0.0"},
		"/zend1-ini":  {Name: "zend1"},
		"/laravel":    {Name: "laravel", Version: "11.9.0"},
		"/nuxt":       {Name: "nuxt", Version: "3.12.0"},
		"/custom":     {Name: "custom"},
		"/plain":      nil,
	}
	for root, want := range tests {
		assert.Equal(t, want, framework(f, root, all), root)
	}
	assert.Equal(t, "symfony", all[3].name, "a declared detector replaces the built-in one")
	assert.Len(t, all, len(declared)+len(builtinDetectors)-1)
}

func TestNewer(t *testing.T) {
	assert.True(t, newer("20.11.1", "20.9.0"))
	assert.False(t, newer("20.9.0", "20.11.1"))
	assert.True(t, newer("20.1.1", "20.1"))
}

func TestPathEnv(t *testing.T) {
	for p, want := range map[string]string{
		"/home/svc/var/www/example.fr/api/production/www": "production",
		"/srv/prod/apps/staging/x":                        "staging",
		"/srv/apps/shop/pp/www":                           "",
		"/srv/apps/production-old/www":                    "",
		"/var/www/preview":                                "preview",
	} {
		assert.Equal(t, want, pathEnv(p), p)
	}
}

func TestReadDeployInfo_YML(t *testing.T) {
	f := fakefs.New()
	f.Files(t, map[string]string{"/a/.app.lacis.yml": "env: pp\nversion: v1\ncommit: abc\n", "/b/.app.lacis.yaml": "app: x\n", "/b/.app.lacis.yml": "app: y\n"})
	info, warnings := readDeployInfo(f, "/a")
	require.Empty(t, warnings)
	assert.Equal(t, "pp", info.Env)
	info, warnings = readDeployInfo(f, "/b")
	assert.Equal(t, "x", info.App, ".yaml before .yml")
	assert.ErrorContains(t, warnings[0], "several version files (.app.lacis.yaml, .app.lacis.yml)")
}
