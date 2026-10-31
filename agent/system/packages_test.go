package system

import (
	"errors"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/fakefs"
	mockExec "github.com/alexandreh2ag/lacis/agent/mocks/exec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newSystem returns a System on a fake file system and a mocked executor.
func newSystem(t *testing.T, family Family, files map[string]string) (*System, *fakefs.FS, *mockExec.MockExecutor) {
	ctx := context.TestContext(nil)
	f := fakefs.New()
	f.Files(t, files)
	ctx.Fs = f
	e := mockExec.NewMockExecutor(gomock.NewController(t))
	ctx.Executor = e
	return &System{Family: family, ctx: ctx}, f, e
}

func TestPackages(t *testing.T) {
	dpkgArgs := []any{"-W", "-f", `${db:Status-Abbrev}\t${Package}\t${Version}\n`}
	rpmArgs := []any{"-qa", "--qf", `%{NAME}\t%{VERSION}-%{RELEASE}\n`}
	tests := []struct {
		name     string
		family   Family
		commands []string
		expect   func(e *mockExec.MockExecutor)
		want     Packages
	}{
		{
			name:     "dpkg",
			family:   FamilyDebian,
			commands: []string{DpkgQueryCommand, RPMCommand},
			expect: func(e *mockExec.MockExecutor) {
				e.EXPECT().Run(gomock.Any(), DpkgQueryCommand, dpkgArgs...).Return([]byte(
					"ii \tnginx\t1.22.1-9+deb12u1\nrc \tapache2\t2.4.62-1\nii \tphp8.2-fpm\t8.2.24-1\nhi \tclickhouse-server\t24.8.4.13\ni\tshort\t1\nbroken line\n"), nil)
			},
			want: Packages{"nginx": "1.22.1-9+deb12u1", "php8.2-fpm": "8.2.24-1", "clickhouse-server": "24.8.4.13"},
		},
		{
			name:     "rpm first on rhel",
			family:   FamilyRHEL,
			commands: []string{DpkgQueryCommand, RPMCommand},
			expect: func(e *mockExec.MockExecutor) {
				e.EXPECT().Run(gomock.Any(), RPMCommand, rpmArgs...).Return([]byte("nginx\t1.20.1-14.el9\ngpg-pubkey\n"), nil)
			},
			want: Packages{"nginx": "1.20.1-14.el9"},
		},
		{
			name:     "rpm on an unknown family without dpkg",
			family:   FamilyUnknown,
			commands: []string{RPMCommand},
			expect: func(e *mockExec.MockExecutor) {
				e.EXPECT().Run(gomock.Any(), RPMCommand, rpmArgs...).Return(nil, nil)
			},
			want: Packages{},
		},
		{name: "no package manager", family: FamilyUnknown, want: Packages{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{}
			for _, c := range tt.commands {
				files[c] = ""
			}
			sys, _, e := newSystem(t, tt.family, files)
			if tt.expect != nil {
				tt.expect(e)
			}
			got, err := sys.Packages(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			again, err := sys.Packages(t.Context())
			require.NoError(t, err)
			assert.Equal(t, got, again, "read once per cycle")
		})
	}
}

func TestPackages_Errors(t *testing.T) {
	for _, command := range []string{DpkgQueryCommand, RPMCommand} {
		t.Run(command, func(t *testing.T) {
			sys, _, e := newSystem(t, FamilyUnknown, map[string]string{command: ""})
			e.EXPECT().Run(gomock.Any(), command, gomock.Any()).Return(nil, errors.New("database locked"))
			_, err := sys.Packages(t.Context())
			assert.EqualError(t, err, "list packages: database locked")
		})
	}
}

func TestUpstreamVersion(t *testing.T) {
	for in, want := range map[string]string{
		"1:10.11.6-0+deb12u1": "10.11.6",
		"5:7.0.15-1~deb12u1":  "7.0.15",
		"1.20.1-14.el9":       "1.20.1",
		"8.15.0":              "8.15.0",
		"7.0.15+dfsg-1":       "7.0.15",
	} {
		assert.Equal(t, want, UpstreamVersion(in), in)
	}
}

func TestPackagesVersion(t *testing.T) {
	p := Packages{"nginx-core": "1.22.1-9", "nginx-common": "1.22.1-9"}
	v, pkg := p.Version("nginx", "nginx-core", "nginx-common")
	assert.Equal(t, "1.22.1", v)
	assert.Equal(t, "nginx-core", pkg)
	v, pkg = p.Version("apache2")
	assert.Empty(t, v)
	assert.Empty(t, pkg)
}
