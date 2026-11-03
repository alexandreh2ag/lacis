package system

import (
	"testing"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		wantOS     *snapshot.OS
		wantFamily Family
	}{
		{
			name: "debian",
			files: map[string]string{"/etc/os-release": `PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
NAME="Debian GNU/Linux"
VERSION_ID="12"
ID=debian
`},
			wantOS:     &snapshot.OS{ID: "debian", Version: "12", Pretty: "Debian GNU/Linux 12 (bookworm)"},
			wantFamily: FamilyDebian,
		},
		{
			name: "ubuntu, quoted id_like",
			files: map[string]string{"/etc/os-release": `ID=ubuntu
ID_LIKE=debian
VERSION_ID="24.04"
`},
			wantOS:     &snapshot.OS{ID: "ubuntu", IDLike: []string{"debian"}, Version: "24.04"},
			wantFamily: FamilyDebian,
		},
		{
			name: "rocky from /usr/lib, comments and single quotes",
			files: map[string]string{"/usr/lib/os-release": `# comment
ID='rocky'
ID_LIKE="rhel centos fedora"
VERSION_ID="9.4"
`},
			wantOS:     &snapshot.OS{ID: "rocky", IDLike: []string{"rhel", "centos", "fedora"}, Version: "9.4"},
			wantFamily: FamilyRHEL,
		},
		{
			name:       "alpine",
			files:      map[string]string{"/etc/os-release": "ID=alpine\nVERSION_ID=3.20.0\n"},
			wantOS:     &snapshot.OS{ID: "alpine", Version: "3.20.0"},
			wantFamily: FamilyUnknown,
		},
		{name: "no file", wantFamily: FamilyUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TestContext(nil)
			for path, content := range tt.files {
				require.NoError(t, afero.WriteFile(ctx.Fs, path, []byte(content), 0o644))
			}
			sys, err := Load(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.wantOS, sys.OS)
			assert.Equal(t, tt.wantFamily, sys.Family)
		})
	}
}

func TestLoad_Unreadable(t *testing.T) {
	ctx := context.TestContext(nil)
	ctx.Fs = failingFs{ctx.Fs}
	_, err := Load(ctx)
	assert.ErrorContains(t, err, "read /etc/os-release")
}

// failingFs fails every open with an error other than "not exist".
type failingFs struct{ afero.Fs }

func (failingFs) Open(string) (afero.File, error) { return nil, assert.AnError }
