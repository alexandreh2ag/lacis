package system

import (
	"testing"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const messenger = `; Symfony workers
[program:client-api-messenger]
command=php /var/www/client/current/bin/console messenger:consume async
  --time-limit=3600 ; restarted every hour
directory=/var/www/client/current
user=deploy
numprocs=2
environment=APP_ENV="prod",DATABASE_URL="postgresql://db.internal:5432/client?a=1,b=2", LOG_LEVEL=info
autorestart=true

[group:workers]
programs=client-api-messenger

[program:front]
command=/home/deploy/.nvm/nvm-exec npm start
directory=/var/www/front/current
environment=NODE_ENV='production',PORT=3000
`

func TestLaunchers(t *testing.T) {
	sys, _, _ := newSystem(t, FamilyDebian, map[string]string{
		"/etc/supervisor/conf.d/client.conf": messenger,
		"/etc/supervisor/conf.d/README":      "[program:ignored]\ncommand=x\n",
		"/etc/supervisord.d/legacy.ini":      "[supervisord]\nlogfile=/var/log/s.log\n[program:cron]\ncommand=/usr/sbin/cron -f\n",
	})
	got, err := sys.Launchers()
	require.NoError(t, err)
	assert.Equal(t, []Launcher{
		{
			Launcher: snapshot.Launcher{
				Type: "supervisor", Name: "client-api-messenger",
				Command:   "php /var/www/client/current/bin/console messenger:consume async --time-limit=3600",
				Directory: "/var/www/client/current", User: "deploy",
			},
			Environment: map[string]string{"APP_ENV": "prod", "DATABASE_URL": "postgresql://db.internal:5432/client?a=1,b=2", "LOG_LEVEL": "info"},
			File:        "/etc/supervisor/conf.d/client.conf",
		},
		{
			Launcher:    snapshot.Launcher{Type: "supervisor", Name: "cron", Command: "/usr/sbin/cron -f"},
			Environment: map[string]string{},
			File:        "/etc/supervisord.d/legacy.ini",
		},
		{
			Launcher: snapshot.Launcher{
				Type: "supervisor", Name: "front", Command: "/home/deploy/.nvm/nvm-exec npm start", Directory: "/var/www/front/current",
			},
			Environment: map[string]string{"NODE_ENV": "production", "PORT": "3000"},
			File:        "/etc/supervisor/conf.d/client.conf",
		},
	}, got)
}

func TestLaunchers_None(t *testing.T) {
	sys, _, _ := newSystem(t, FamilyDebian, nil)
	got, err := sys.Launchers()
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestParseEnvironment(t *testing.T) {
	tests := []struct {
		in   string
		want map[string]string
	}{
		{in: "", want: map[string]string{}},
		{in: `A=1`, want: map[string]string{"A": "1"}},
		{in: `A="x,y", B = 2 ,C='z'`, want: map[string]string{"A": "x,y", "B": "2", "C": "z"}},
		{in: `A="unterminated`, want: map[string]string{"A": "unterminated"}},
		{in: `=orphan,B=1`, want: map[string]string{"B": "1"}},
		{in: `garbage`, want: map[string]string{}},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, parseEnvironment(tt.in), tt.in)
	}
}

const supervisord = `[unix_http_server]
file=/var/run/supervisor.sock

[supervisord]
logfile=/var/log/supervisor/supervisord.log

[include]
files = /etc/supervisor/conf.d/*.conf %(here)s/apps/*.ini relative/*.conf ; comment

[program:main-file]
command=/usr/bin/true
`

func TestLaunchers_MainFile(t *testing.T) {
	sys, _, _ := newSystem(t, FamilyDebian, map[string]string{
		"/etc/supervisor/supervisord.conf":    supervisord,
		"/etc/supervisor/conf.d/a.conf":       "[program:a]\ncommand=a\n",
		"/etc/supervisor/apps/pgc.ini":        "[group:pgc]\nprograms=consumer\n[program:consumer]\ncommand=php /home/web/www/bin/console messenger:consume\n",
		"/etc/supervisor/relative/r.conf":     "[program:r]\ncommand=r\n",
		"/etc/supervisor/conf.d/ignored.txt":  "[program:ignored]\n",
		"/etc/supervisord.d/not-included.ini": "[program:not-included]\n",
	})
	got, err := sys.Launchers()
	require.NoError(t, err)
	var names []string
	for _, l := range got {
		names = append(names, l.Name)
	}
	assert.Equal(t, []string{"a", "consumer", "main-file", "r"}, names)
}

func TestLaunchers_RunningConfig(t *testing.T) {
	sys, f, _ := newSystem(t, FamilyDebian, map[string]string{
		"/opt/sv/sv.conf":                  "[include]\nfiles=progs/*.conf\n",
		"/opt/sv/progs/p.conf":             "[program:p]\ncommand=p\n",
		"/etc/supervisor/supervisord.conf": "[program:default]\ncommand=d\n",
		"/proc/50/status":                  "Name:\tsupervisord\nPPid:\t1\n",
		"/proc/50/cmdline":                 "/usr/bin/python3\x00/usr/bin/supervisord\x00-n\x00-c\x00/opt/sv/sv.conf\x00",
	})
	f.Link("/proc/50/exe", "/usr/bin/python3.11")
	got, err := sys.Launchers()
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "p", got[0].Name, "the -c of the running supervisord wins")
}

func TestSupervisorMain_LongFlag(t *testing.T) {
	sys, f, _ := newSystem(t, FamilyDebian, map[string]string{
		"/proc/50/status":  "Name:\tsupervisord\nPPid:\t1\n",
		"/proc/50/cmdline": "/usr/local/bin/supervisord\x00--configuration=/srv/sv.conf\x00",
	})
	f.Link("/proc/50/exe", "/usr/bin/python3.11")
	assert.Equal(t, "/srv/sv.conf", sys.supervisorMain())
}

func TestLaunchers_MissingMainFile(t *testing.T) {
	sys, f, _ := newSystem(t, FamilyDebian, map[string]string{
		"/proc/50/status":  "Name:\tsupervisord\nPPid:\t1\n",
		"/proc/50/cmdline": "supervisord\x00-c\x00/gone.conf\x00",
	})
	f.Link("/proc/50/exe", "/usr/bin/python3.11")
	_, err := sys.Launchers()
	assert.ErrorContains(t, err, "read /gone.conf")
}

func TestLaunchers_PIDs(t *testing.T) {
	sys, f, _ := newSystem(t, FamilyDebian, map[string]string{
		"/etc/supervisor/conf.d/front.conf": "[program:start]\ncommand=bash -c \"npm run start\"\nnumprocs=2\n[program:gtask]\ncommand=node gtask.js\n[program:stopped]\ncommand=sleep 1\n",
		"/proc/50/status":                   "Name:\tsupervisord\nPPid:\t1\n",
		"/proc/50/cmdline":                  "/usr/bin/python3\x00/usr/bin/supervisord\x00-n\x00",
		"/proc/60/status":                   "Name:\tbash\nPPid:\t50\n",
		"/proc/60/cmdline":                  "bash\x00-c\x00npm run start\x00",
		"/proc/61/status":                   "Name:\tbash\nPPid:\t50\n",
		"/proc/61/cmdline":                  "bash\x00-c\x00npm   run start\x00",
		"/proc/62/status":                   "Name:\tnode\nPPid:\t50\n",
		"/proc/62/cmdline":                  "node\x00gtask.js\x00",
		"/proc/70/status":                   "Name:\tnode\nPPid:\t1\n",
		"/proc/70/cmdline":                  "node\x00gtask.js\x00",
		// nvm-exec runs node with exec: only the environment tells it.
		"/etc/supervisor/conf.d/next.conf": "[program:next]\ncommand=/var/lib/nvm/nvm-exec ./node_modules/.bin/next start\n[program:multi]\ncommand=worker\nnumprocs=2\nprocess_name=%(program_name)s_%(process_num)02d\n",
		"/proc/80/status":                  "Name:\tnext-server\nPPid:\t50\n",
		"/proc/80/cmdline":                 "next-server (v15.5.24)\x00",
		"/proc/80/environ":                 "HOME=/home/web\x00SUPERVISOR_ENABLED=1\x00SUPERVISOR_PROCESS_NAME=next\x00SUPERVISOR_GROUP_NAME=next\x00",
		"/proc/81/status":                  "Name:\tworker\nPPid:\t50\n",
		"/proc/81/cmdline":                 "worker\x00",
		"/proc/81/environ":                 "SUPERVISOR_PROCESS_NAME=multi_01\x00",
	})
	for _, pid := range []string{"50", "60", "61", "62", "70", "80", "81"} {
		f.Link("/proc/"+pid+"/exe", "/usr/bin/x")
	}
	got, err := sys.Launchers()
	require.NoError(t, err)
	pids := map[string][]int{}
	for _, l := range got {
		pids[l.Name] = l.PIDs
	}
	assert.Equal(t, map[string][]int{"gtask": {62}, "start": {60, 61}, "stopped": nil, "next": {80}, "multi": {81}}, pids, "not started by supervisord: 70 left out")
}

func TestProgramOf(t *testing.T) {
	launchers := []Launcher{
		{Launcher: snapshot.Launcher{Name: "consumer", Command: "php a"}},
		{Launcher: snapshot.Launcher{Name: "consumer_async", Command: "php b"}},
	}
	assert.Equal(t, 1, programOf(launchers, "consumer_async", ""), "exact name first")
	assert.Equal(t, 0, programOf(launchers, "consumer_03", ""), "numprocs instance")
	assert.Equal(t, -1, programOf(launchers, "consumer_x", ""))
	assert.Equal(t, 1, programOf(launchers, "", "php  b"), "by command without name")
	assert.Equal(t, -1, programOf(launchers, "", "php c"))
}
