package system

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/exec"
	"github.com/alexandreh2ag/lacis/agent/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestIsSupervisord(t *testing.T) {
	tests := []struct {
		name string
		p    Process
		want bool
	}{
		{name: "binary", p: Process{Exe: "/usr/bin/supervisord", Cmdline: []string{"/usr/bin/supervisord", "-n"}}, want: true},
		{name: "argv0", p: Process{Exe: "/usr/bin/python3.11", Cmdline: []string{"supervisord", "-c", "x"}}, want: true},
		{name: "python script", p: Process{Exe: "/usr/bin/python3.11", Cmdline: []string{"/usr/bin/python3", "/usr/bin/supervisord", "-n"}}, want: true},
		{name: "title", p: Process{Exe: "/usr/bin/python3.11", Cmdline: []string{"/usr/bin/python3 /usr/local/bin/supervisord -n"}}, want: true},
		{name: "any argument", p: Process{Exe: "/usr/bin/vim", Cmdline: []string{"vim", "/tmp/supervisord"}}},
		{name: "argument of python", p: Process{Exe: "/usr/bin/python3", Cmdline: []string{"python3", "x.py", "supervisord"}}},
		{name: "empty", p: Process{Exe: "/usr/bin/x"}},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, isSupervisord(tt.p), tt.name)
	}
}

func TestLaunchers_FaultyFileSkipped(t *testing.T) {
	sys, _, _ := newSystem(t, FamilyDebian, map[string]string{
		"/etc/supervisor/conf.d/a.conf":    "[program:a]\ncommand=a\n",
		"/etc/supervisor/conf.d/long.conf": "[program:long]\ncommand=" + strings.Repeat("x", 2<<20) + "\n",
	})
	got, err := sys.Launchers()
	assert.ErrorContains(t, err, "read /etc/supervisor/conf.d/long.conf")
	require.Len(t, got, 1)
	assert.Equal(t, "a", got[0].Name)
}

func TestLaunchers_LongLine(t *testing.T) {
	long := strings.Repeat("x", 100_000)
	sys, _, _ := newSystem(t, FamilyDebian, map[string]string{
		"/etc/supervisor/conf.d/a.conf": "[program:a]\ncommand=" + long + "\n",
	})
	got, err := sys.Launchers()
	require.NoError(t, err)
	assert.Equal(t, long, got[0].Command)
}

func TestLaunchers_UserSupervisord(t *testing.T) {
	// A supervisord of a user: its -c is read as that user, so a fake one
	// cannot make the agent read a file of root.
	sys, f, e := newSystem(t, FamilyDebian, map[string]string{
		"/proc/50/status":  "Name:\tsupervisord\nPPid:\t1\nUid:\t1000\t1000\t1000\t1000\nGid:\t1000\t1000\t1000\t1000\nGroups:\t\n",
		"/proc/50/cmdline": "/home/bob/bin/supervisord\x00-c\x00/root/.aws/credentials\x00",
	})
	f.Link("/proc/50/exe", "/home/bob/bin/supervisord")
	e.EXPECT().ReadFileAs(gomock.Any(), exec.Credential{UID: 1000, GID: 1000, Groups: []uint32{}}, "/root/.aws/credentials", fsutil.MaxConfigSize).
		Return(nil, &fs.PathError{Op: "open", Path: "/root/.aws/credentials", Err: fs.ErrPermission})
	_, err := sys.Launchers()
	assert.ErrorIs(t, err, fs.ErrPermission)
}
