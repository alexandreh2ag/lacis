package services

import (
	"testing"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollect_ObjectStores(t *testing.T) {
	tests := []struct {
		name  string
		setup func(h *host)
		want  []snapshot.Service
	}{
		{
			name: "from packages",
			setup: func(h *host) {
				h.process(100, 1, "/usr/local/bin/minio", "/usr/local/bin/minio", "server", "/srv/minio", "--console-address", ":9001")
				h.listen(100, "0.0.0.0:9000")
				h.pkg("minio", "20240116160738.0.0")
				h.process(200, 1, "/usr/bin/silo", "/usr/bin/silo", "server", "/data")
				h.listen(200, "0.0.0.0:9100")
				h.pkg("silo", "20261026.0.0")
				// The healthcheck is not a server.
				h.process(300, 1, "/usr/bin/silo", "/usr/bin/silo", "healthcheck", "ready")
			},
			want: []snapshot.Service{
				{Type: "minio", Version: "RELEASE.2024-01-16T16-07-38Z", Listen: []string{"0.0.0.0:9000"}},
				{Type: "silo", Version: "20261026.0.0", Listen: []string{"0.0.0.0:9100"}},
			},
		},
		{
			name: "from binaries",
			setup: func(h *host) {
				h.process(100, 1, "/opt/minio/minio", "/opt/minio/minio", "server", "/data")
				h.versions["/opt/minio/minio"] = "minio version RELEASE.2025-04-22T22-12-26Z (commit-id=0d7408fc9969caf07de6a8c3a84f9fbb10a6739e)\nRuntime: go1.24.2 linux/amd64\n"
				h.process(200, 1, "/opt/silo/silo", "/opt/silo/silo", "server", "/data")
				h.versions["/opt/silo/silo"] = "silo version RELEASE.2026-10-26T00-00-00Z (commit-id=abc)\n"
			},
			want: []snapshot.Service{
				{Type: "minio", Version: "RELEASE.2025-04-22T22-12-26Z"},
				{Type: "silo", Version: "RELEASE.2026-10-26T00-00-00Z"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHost(t)
			tt.setup(h)
			out, _, err := collect(t, h.context(nil))
			require.NoError(t, err)
			assert.Equal(t, tt.want, out.Services)
		})
	}
}
