package system

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/exec"
	"github.com/alexandreh2ag/lacis/agent/fakefs"
	"github.com/alexandreh2ag/lacis/agent/fsutil"
	mockExec "github.com/alexandreh2ag/lacis/agent/mocks/exec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCredential(t *testing.T) {
	cred, ok := Credential(Process{UID: 1000, GID: 33, Groups: []int{4, 1000}})
	assert.True(t, ok)
	assert.Equal(t, exec.Credential{UID: 1000, GID: 33, Groups: []uint32{4, 1000}}, cred)

	cred, ok = Credential(Process{UID: 1000, GID: 1000})
	assert.True(t, ok)
	assert.Equal(t, []uint32{}, cred.Groups, "never nil: the groups of root are dropped")

	_, ok = Credential(Process{UID: -1, GID: 0})
	assert.False(t, ok)
	_, ok = Credential(Process{UID: 0, GID: -1})
	assert.False(t, ok)
}

func TestReadAs(t *testing.T) {
	sys, f, e := newSystem(t, FamilyDebian, map[string]string{"/etc/supervisor/supervisord.conf": "[include]\n"})
	f.Link("/etc/supervisor/link.conf", "/dev/zero")

	t.Run("root process: read directly", func(t *testing.T) {
		b, err := sys.ReadAs(t.Context(), Process{UID: 0, GID: 0}, "/etc/supervisor/supervisord.conf")
		require.NoError(t, err)
		assert.Equal(t, "[include]\n", string(b))
	})
	t.Run("user process: read as the user", func(t *testing.T) {
		cred := exec.Credential{UID: 1000, GID: 1000, Groups: []uint32{33}}
		e.EXPECT().ReadFileAs(gomock.Any(), cred, "/root/.aws/credentials", fsutil.MaxConfigSize).
			Return(nil, &fs.PathError{Op: "open", Path: "/root/.aws/credentials", Err: fs.ErrPermission})
		_, err := sys.ReadAs(t.Context(), Process{UID: 1000, GID: 1000, Groups: []int{33}}, "/root/.aws/credentials")
		assert.ErrorIs(t, err, fs.ErrPermission)
	})
	t.Run("unknown user: not read", func(t *testing.T) {
		_, err := sys.ReadAs(t.Context(), Process{UID: 1000, GID: -1}, "/etc/x")
		assert.ErrorIs(t, err, ErrUnknownUser)
	})
}

func TestProbe(t *testing.T) {
	denied := &fs.PathError{Op: "fork/exec", Path: "/home/bob/bin/php", Err: fs.ErrPermission}
	bob := &Process{UID: 1000, GID: 1000}
	bobCred := exec.Credential{UID: 1000, GID: 1000, Groups: []uint32{}}
	root := &Process{UID: 0, GID: 0}
	rootCred := exec.Credential{UID: 0, GID: 0, Groups: []uint32{}}

	tests := []struct {
		name    string
		process *Process
		// owner of the binary, -1 when the file system does not tell
		owner   int
		mode    os.FileMode
		expect  func(e *execExpect)
		want    string
		wantErr error
	}{
		{
			name: "nobody runs it", process: bob,
			expect: func(e *execExpect) { e.nobody([]byte("PHP 8.2.1"), nil) },
			want:   "PHP 8.2.1",
		},
		{
			name: "nobody fails otherwise: no fallback", process: bob,
			expect:  func(e *execExpect) { e.nobody(nil, errors.New("exit status 1")) },
			wantErr: errors.New("exit status 1"),
		},
		{
			name: "nobody denied: run as the user of the process", process: bob,
			expect: func(e *execExpect) {
				e.nobody(nil, denied)
				e.as(bobCred, []byte("PHP 8.3.0"), nil)
			},
			want: "PHP 8.3.0",
		},
		{
			name: "nobody denied, no process", process: nil,
			expect:  func(e *execExpect) { e.nobody(nil, denied) },
			wantErr: fs.ErrPermission,
		},
		{
			name: "nobody denied, unknown user", process: &Process{UID: -1, GID: -1},
			expect:  func(e *execExpect) { e.nobody(nil, denied) },
			wantErr: fs.ErrPermission,
		},
		{
			name: "root process, binary of root", process: root, owner: 0, mode: 0o755,
			expect: func(e *execExpect) {
				e.nobody(nil, denied)
				e.as(rootCred, []byte("v=7.0.15"), nil)
			},
			want: "v=7.0.15",
		},
		{
			name: "root process, binary of another user", process: root, owner: 1000, mode: 0o755,
			expect:  func(e *execExpect) { e.nobody(nil, denied) },
			wantErr: ErrUnsafeProbe,
		},
		{
			name: "root process, binary writable by all", process: root, owner: 0, mode: 0o777,
			expect:  func(e *execExpect) { e.nobody(nil, denied) },
			wantErr: ErrUnsafeProbe,
		},
		{
			name: "root process, binary writable by a group", process: root, owner: 0, mode: 0o775,
			expect:  func(e *execExpect) { e.nobody(nil, denied) },
			wantErr: ErrUnsafeProbe,
		},
		{
			name: "root process, owner unknown", process: root, owner: -1, mode: 0o755,
			expect:  func(e *execExpect) { e.nobody(nil, denied) },
			wantErr: ErrUnsafeProbe,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const exe = "/home/bob/bin/php"
			sys, f, e := newSystem(t, FamilyDebian, map[string]string{exe: "#!"})
			require.NoError(t, f.Chmod(exe, tt.mode))
			owner := tt.owner
			saved := fileOwner
			fileOwner = func(fs.FileInfo) (uint32, uint32, bool) { return uint32(owner), 1000, owner >= 0 }
			t.Cleanup(func() { fileOwner = saved })
			tt.expect(&execExpect{e: e, exe: exe})

			out, err := sys.Probe(t.Context(), tt.process, exe, "-n", "-v")
			if tt.wantErr != nil {
				if errors.Is(tt.wantErr, fs.ErrPermission) || errors.Is(tt.wantErr, ErrUnsafeProbe) {
					assert.ErrorIs(t, err, tt.wantErr)
				} else {
					assert.EqualError(t, err, tt.wantErr.Error())
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(out))
		})
	}
}

func TestWritableByOthers(t *testing.T) {
	sys, _, _ := newSystem(t, FamilyDebian, nil)
	assert.True(t, sys.writableByOthers("/missing"))
	// A file of the memory file system has no owner.
	var f *fakefs.FS
	sys, f, _ = newSystem(t, FamilyDebian, map[string]string{"/usr/bin/php": ""})
	require.NoError(t, f.Chmod("/usr/bin/php", 0o755))
	assert.True(t, sys.writableByOthers("/usr/bin/php"))
	// The owner of a real file.
	fi, err := os.Stat(t.TempDir())
	require.NoError(t, err)
	uid, _, ok := fileOwner(fi)
	require.True(t, ok)
	assert.Equal(t, uint32(os.Getuid()), uid)
}

type execExpect struct {
	e   *mockExec.MockExecutor
	exe string
}

func (x *execExpect) nobody(out []byte, err error) {
	x.as(exec.Nobody, out, err)
}

func (x *execExpect) as(cred exec.Credential, out []byte, err error) {
	x.e.EXPECT().RunAs(gomock.Any(), cred, x.exe, "-n", "-v").Return(out, err)
}
