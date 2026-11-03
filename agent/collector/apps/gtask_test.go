package apps

import (
	"strings"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollect_MaxFiles(t *testing.T) {
	tests := []struct {
		name     string
		maxFiles int
		want     []string
		wantErr  string
	}{
		{name: "under the bound", maxFiles: 100, want: []string{"/srv/apps/a", "/srv/apps/b", "/srv/apps/c/www"}},
		{
			// a, b, c, then the entries of c: none is read.
			name: "bound reached", maxFiles: 3, want: []string{"/srv/apps/a", "/srv/apps/b"},
			wantErr: "scan of /srv/apps stopped after 3 entries: collectors.apps.max_files reached",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHost(t)
			h.fs.Files(t, map[string]string{
				"/srv/apps/a/composer.json":     "{}",
				"/srv/apps/b/package.json":      "{}",
				"/srv/apps/c/www/composer.json": "{}",
			})
			h.configure = func(c *config.Config) { c.Collectors.Apps.MaxFiles = tt.maxFiles }
			out := &snapshot.Snapshot{}
			err := h.collect(out, "/srv/apps")
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
			var paths []string
			for _, a := range out.Apps {
				paths = append(paths, a.Path)
			}
			assert.Equal(t, tt.want, paths)
		})
	}
}

func TestCollect_GtaskTasks(t *testing.T) {
	h := newHost(t)
	h.fs.Files(t, map[string]string{
		"/srv/shop/composer.json": "{}",
		"/srv/shop/.reflet/gtask.yml": "scheduled:\n" +
			"  - id: sitemap\n    expr: \"0 3 * * *\"\n    command: \"php bin/console app:sitemap --token=SECRET\"\n" +
			"  - id: cleanup\n    expr: \"*/5 * * * *\"\n    command: \"php bin/console app:cleanup\"\n" +
			"workers:\n  - id: consumer\n    command: \"php bin/console messenger:consume\"\n",
		"/srv/other/package.json": "{}",
		"/srv/other/broken.yml":   "scheduled: [\n",
		"/etc/secret.yml":         "scheduled:\n  - id: leaked\n    expr: \"* * * * *\"\n",
		"/etc/supervisor/conf.d/shop.conf": "[program:shop-cron]\ncommand=/usr/local/bin/gtask schedule start --tick 5m -c .reflet/gtask.yml\ndirectory=/srv/shop\n" +
			"[program:shop-worker]\ncommand=gtask worker generate --config .reflet/gtask.yml\ndirectory=/srv/shop\n" +
			"[program:other-cron]\ncommand=gtask schedule start --config=/etc/secret.yml\ndirectory=/srv/other\n" +
			"[program:other-broken]\ncommand=gtask schedule run -c broken.yml\ndirectory=/srv/other\n",
	})
	out := &snapshot.Snapshot{}
	err := h.collect(out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gtask config /etc/secret.yml of other-cron: out of the app, not read")
	assert.Contains(t, err.Error(), "gtask config /srv/other/broken.yml: yaml")
	require.Len(t, out.Apps, 2)
	assert.Equal(t, []snapshot.Launcher{
		{Type: "supervisor", Name: "other-broken", Role: "worker", Command: "gtask", Directory: "/srv/other"},
		{Type: "supervisor", Name: "other-cron", Role: "worker", Command: "gtask", Directory: "/srv/other"},
	}, out.Apps[0].Launchers)
	assert.Equal(t, []snapshot.Launcher{
		{Type: "supervisor", Name: "shop-cron", Role: "worker", Command: "/usr/local/bin/gtask", Directory: "/srv/shop",
			Tasks: []snapshot.LauncherTask{{ID: "sitemap", Schedule: "0 3 * * *"}, {ID: "cleanup", Schedule: "*/5 * * * *"}}},
		{Type: "supervisor", Name: "shop-worker", Role: "worker", Command: "gtask", Directory: "/srv/shop"},
	}, out.Apps[1].Launchers)
}

func TestCollect_GtaskTaskCommands(t *testing.T) {
	long := strings.Repeat("x", maxGtaskCommand+1)
	tests := []struct {
		name string
		mode config.CommandMode
		want []snapshot.LauncherTask
	}{
		{name: "path: never read", mode: config.CommandPath, want: []snapshot.LauncherTask{
			{ID: "sitemap", Schedule: "0 3 * * *"}, {ID: "long", Schedule: "* * * * *"}}},
		{name: "full: read, a long one left out", mode: config.CommandFull, want: []snapshot.LauncherTask{
			{ID: "sitemap", Schedule: "0 3 * * *", Command: "php bin/console app:sitemap --token=SECRET"},
			{ID: "long", Schedule: "* * * * *"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHost(t)
			h.fs.Files(t, map[string]string{
				"/srv/shop/composer.json": "{}",
				"/srv/shop/.reflet/gtask.yml": "scheduled:\n" +
					"  - id: sitemap\n    expr: \"0 3 * * *\"\n    command: \"php bin/console app:sitemap --token=SECRET\"\n" +
					"    envs:\n      DATABASE_URL: mysql://root:SECRET@db/shop\n" +
					"  - id: long\n    expr: \"* * * * *\"\n    command: " + long + "\n",
				"/etc/supervisor/conf.d/shop.conf": "[program:shop-cron]\ncommand=gtask schedule start -c .reflet/gtask.yml\ndirectory=/srv/shop\n",
			})
			h.configure = func(c *config.Config) { c.Collectors.Apps.Commands = tt.mode }
			out := &snapshot.Snapshot{}
			require.NoError(t, h.collect(out))
			require.Len(t, out.Apps, 1)
			require.Len(t, out.Apps[0].Launchers, 1)
			assert.Equal(t, tt.want, out.Apps[0].Launchers[0].Tasks)
		})
	}
}

func TestCollect_GtaskTasksThroughLink(t *testing.T) {
	h := newHost(t)
	h.fs.Files(t, map[string]string{
		"/home/web/builds/front/staging/www/package.json":       "{}",
		"/home/web/builds/front/staging/www/.reflet/gtask.yml":  "scheduled:\n  - id: sitemap\n    expr: \"0 3 * * *\"\n",
		"/home/web/builds/front/staging/www/.reflet/escape.yml": "",
		"/etc/secret.yml": "scheduled:\n  - id: leaked\n    expr: \"* * * * *\"\n",
		"/etc/supervisor/conf.d/front.conf": "[program:front-cron]\n" +
			"command=gtask schedule start -c /home/web/var/www/front/staging/www/.reflet/gtask.yml\n" +
			"directory=/home/web/builds/front/staging/www\n" +
			"[program:front-escape]\n" +
			"command=gtask schedule start -c /home/web/var/www/front/staging/www/.reflet/escape.yml\n" +
			"directory=/home/web/builds/front/staging/www\n",
	})
	// The configuration is given through a link to the builds.
	h.fs.Link("/home/web/var/www/front", "/home/web/builds/front")
	// A link of the app out of it is not followed.
	require.NoError(t, h.fs.Remove("/home/web/builds/front/staging/www/.reflet/escape.yml"))
	h.fs.Link("/home/web/builds/front/staging/www/.reflet/escape.yml", "/etc/secret.yml")
	out := &snapshot.Snapshot{}
	err := h.collect(out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gtask config /home/web/var/www/front/staging/www/.reflet/escape.yml of front-escape: out of the app, not read")
	assert.NotContains(t, err.Error(), "front-cron")
	require.Len(t, out.Apps, 1)
	assert.Equal(t, []snapshot.LauncherTask{{ID: "sitemap", Schedule: "0 3 * * *"}}, out.Apps[0].Launchers[0].Tasks)
	assert.Nil(t, out.Apps[0].Launchers[1].Tasks)
}

func TestGtaskConfigPath(t *testing.T) {
	tests := []struct{ command, want string }{
		{"gtask schedule start -c .reflet/gtask.yml", ".reflet/gtask.yml"},
		{"/usr/bin/gtask schedule start --tick 5m --config /srv/a/gtask.yml", "/srv/a/gtask.yml"},
		{"gtask schedule run --config=gtask.yml", "gtask.yml"},
		{"gtask schedule start", ""},
		{"gtask schedule start -c", ""},
		{"gtask worker generate -c gtask.yml", ""},
		{"node gtask.js", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, gtaskConfigPath(tt.command), tt.command)
	}
}
