package host

import (
	"errors"
	"net/netip"
	"runtime"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/kernel"
	mockExec "github.com/alexandreh2ag/lacis/agent/mocks/exec"
	mockKernel "github.com/alexandreh2ag/lacis/agent/mocks/kernel"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const cpuinfo = `processor	: 0
vendor_id	: AuthenticAMD
model name	: AMD EPYC 7543P 32-Core Processor

processor	: 1
model name	: AMD EPYC 7543P 32-Core Processor

processor	: 2
model name	: AMD EPYC 7543P 32-Core Processor

processor	: 3
model name	: AMD EPYC 7543P 32-Core Processor
`

const meminfo = `MemTotal:       16384000 kB
MemFree:         1024000 kB
`

const mounts = `rpool/data/subvol-101-disk-0 / zfs rw,relatime 0 0
proc /proc proc rw,nosuid 0 0
tmpfs /run tmpfs rw 0 0
/dev/sdb1 /var/lib/my\040data ext4 rw 0 0
/dev/sdb1 /srv/bind ext4 rw 0 0
overlay /var/lib/docker/overlay2/x/merged overlay rw 0 0
`

const route = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
eth0	00000000	0101000A	0003	0	0	0	00000000	0	0	0
eth0	0001000A	00000000	0001	0	0	0	00FFFFFF	0	0	0
eth1	0071000A	00000000	0001	0	0	0	00FFFFFF	0	0	0
`

const ipv6Route = `00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000001 00000000 00000003 eth1
00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 ffffffff 00000001 00000000 00200200     lo
`

func newContext(t *testing.T, files map[string]string) (*context.Context, *mockKernel.MockKernel, *mockExec.MockExecutor) {
	ctx := context.TestContext(nil)
	for path, content := range files {
		require.NoError(t, afero.WriteFile(ctx.Fs, path, []byte(content), 0o644))
	}
	ctrl := gomock.NewController(t)
	k := mockKernel.NewMockKernel(ctrl)
	e := mockExec.NewMockExecutor(ctrl)
	ctx.Kernel, ctx.Executor = k, e
	return ctx, k, e
}

func prefixes(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(s))
	for i, p := range s {
		out[i] = netip.MustParsePrefix(p)
	}
	return out
}

func TestCollect_LXCWithCgroupV2(t *testing.T) {
	ctx, k, e := newContext(t, map[string]string{
		KernelReleaseFile: "6.8.12-4-pve\n",
		ArchFile:          "x86_64\n",
		CPUInfoFile:       cpuinfo,
		MemInfoFile:       meminfo,
		MountsFile:        mounts,
		RouteFile:         route,
		IPv6RouteFile:     ipv6Route,
		CgroupCPUMax:      "150000 100000\n",
		CgroupMemoryMax:   "8589934592\n",
		DetectVirtCommand: "",
	})
	e.EXPECT().Run(gomock.Any(), DetectVirtCommand).Return([]byte("lxc\n"), nil)
	k.EXPECT().DiskSize("/").Return(uint64(64*gib), nil)
	k.EXPECT().DiskSize("/var/lib/my data").Return(uint64(500*gib+5), nil)
	k.EXPECT().Interfaces().Return([]kernel.Interface{
		{Name: "lo", Loopback: true, Up: true, Addrs: prefixes("127.0.0.1/8", "::1/128")},
		{Name: "eth1", Up: true, Addrs: prefixes("10.0.113.10/24", "fe80::1/64", "2001:db8::10/64")},
		{Name: "eth0", Up: true, Addrs: prefixes("10.0.1.12/24")},
		{Name: "eth2", Up: false, Addrs: prefixes("10.0.2.12/24")},
		{Name: "eth3", Up: true},
	}, nil)

	os := &snapshot.OS{ID: "debian", Version: "12"}
	out := &snapshot.Snapshot{Host: snapshot.Host{Hostname: "acme-site-prod-1"}}
	require.NoError(t, (&Collector{}).Collect(t.Context(), ctx, &system.System{OS: os}, out))

	assert.Equal(t, snapshot.Host{
		Hostname:       "acme-site-prod-1",
		OS:             os,
		Kernel:         "6.8.12-4-pve",
		Arch:           "x86_64",
		Virtualization: "lxc",
		CPU:            &snapshot.CPU{Model: "AMD EPYC 7543P 32-Core Processor", VisibleCores: 4, EffectiveCores: 1.5, Source: snapshot.ResourceSourceCgroup},
		MemoryMB:       &snapshot.Memory{Visible: 16000, Effective: 8192, Source: snapshot.ResourceSourceCgroup},
		Disks: []snapshot.Disk{
			{Mount: "/", SizeGB: 64},
			{Mount: "/var/lib/my data", SizeGB: 500},
		},
		Interfaces: []snapshot.Interface{
			{Name: "eth0", IPs: []string{"10.0.1.12/24"}, DefaultRoute: true},
			{Name: "eth1", IPs: []string{"10.0.113.10/24", "2001:db8::10/64"}, DefaultRoute: true},
		},
	}, out.Host)
}

func TestCollect_BareMetalCgroupV1(t *testing.T) {
	ctx, k, _ := newContext(t, map[string]string{
		KernelReleaseFile: "5.10.0-28-amd64\n",
		CPUInfoFile:       cpuinfo,
		MemInfoFile:       meminfo,
		MountsFile:        "/dev/sda1 / ext4 rw 0 0\n",
		RouteFile:         route,
		CgroupV1CPUQuota:  "-1\n",
		CgroupV1CPUPeriod: "100000\n",
		CgroupV1Memory:    "9223372036854771712\n",
	})
	k.EXPECT().DiskSize("/").Return(uint64(100*gib), nil)
	k.EXPECT().Interfaces().Return(nil, nil)

	out := &snapshot.Snapshot{}
	require.NoError(t, (&Collector{}).Collect(t.Context(), ctx, &system.System{}, out))

	assert.Equal(t, goArchToMachine[runtime.GOARCH], out.Host.Arch, "older kernels: the agent architecture")
	assert.Empty(t, out.Host.Virtualization)
	assert.Nil(t, out.Host.OS)
	assert.Equal(t, &snapshot.CPU{Model: "AMD EPYC 7543P 32-Core Processor", VisibleCores: 4, EffectiveCores: 4, Source: snapshot.ResourceSourceProc}, out.Host.CPU)
	assert.Equal(t, &snapshot.Memory{Visible: 16000, Effective: 16000, Source: snapshot.ResourceSourceProc}, out.Host.MemoryMB)
	assert.Empty(t, out.Host.Interfaces)
}

func TestVirtualization(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		run   bool
		out   string
		want  string
	}{
		{name: "bare metal", files: []string{DetectVirtCommand}, run: true, out: "none\n", want: "none"},
		{name: "detect-virt prints nothing, docker", files: []string{DetectVirtCommand, DockerEnvFile}, run: true, want: "docker"},
		{name: "docker", files: []string{DockerEnvFile}, want: "docker"},
		{name: "podman", files: []string{ContainerEnvFile}, want: "podman"},
		{name: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{}
			for _, f := range tt.files {
				files[f] = ""
			}
			ctx, _, e := newContext(t, files)
			if tt.run {
				e.EXPECT().Run(gomock.Any(), DetectVirtCommand).Return([]byte(tt.out), errors.New("exit status 1"))
			}
			assert.Equal(t, tt.want, virtualization(t.Context(), ctx))
		})
	}
}

func TestCgroupCPUs(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  float64
		ok    bool
	}{
		{name: "v2 quota", files: map[string]string{CgroupCPUMax: "200000 100000"}, want: 2, ok: true},
		{name: "v2 no quota", files: map[string]string{CgroupCPUMax: "max 100000"}},
		{name: "v2 garbage", files: map[string]string{CgroupCPUMax: "garbage"}},
		{name: "v1 quota", files: map[string]string{CgroupV1CPUQuota: "50000", CgroupV1CPUPeriod: "100000"}, want: 0.5, ok: true},
		{name: "v1 without period", files: map[string]string{CgroupV1CPUQuota: "50000"}},
		{name: "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _, _ := newContext(t, tt.files)
			got, ok := cgroupCPUs(ctx.Fs)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCollect_PartialErrors(t *testing.T) {
	ctx, k, _ := newContext(t, map[string]string{
		CPUInfoFile: "processor : 0\n",
		MemInfoFile: "MemFree: 12 kB\n",
		MountsFile:  "/dev/sda1 / ext4 rw 0 0\n/dev/sdb1 /data xfs rw 0 0\n",
	})
	k.EXPECT().DiskSize("/").Return(uint64(10*gib), nil)
	k.EXPECT().DiskSize("/data").Return(uint64(0), errors.New("permission denied"))
	k.EXPECT().Interfaces().Return(nil, errors.New("netlink down"))

	out := &snapshot.Snapshot{}
	err := (&Collector{}).Collect(t.Context(), ctx, &system.System{}, out)

	assert.ErrorContains(t, err, "kernel: open "+KernelReleaseFile)
	assert.ErrorContains(t, err, "memory: MemTotal not found")
	assert.ErrorContains(t, err, "disks: statfs /data: permission denied")
	assert.ErrorContains(t, err, "interfaces: netlink down")
	assert.Equal(t, float64(1), out.Host.CPU.VisibleCores, "what could be read is kept")
	assert.Equal(t, []snapshot.Disk{{Mount: "/", SizeGB: 10}}, out.Host.Disks)
}

func TestCollect_MissingFiles(t *testing.T) {
	ctx, k, _ := newContext(t, nil)
	k.EXPECT().Interfaces().Return(nil, nil).AnyTimes()
	err := (&Collector{}).Collect(t.Context(), ctx, &system.System{}, &snapshot.Snapshot{})
	for _, what := range []string{"cpu", "memory", "disks", "interfaces"} {
		assert.ErrorContains(t, err, what+": open")
	}
}

func TestMemory_InvalidTotal(t *testing.T) {
	ctx, _, _ := newContext(t, map[string]string{MemInfoFile: "MemTotal: lots kB\n"})
	_, err := memory(ctx.Fs)
	assert.ErrorContains(t, err, "MemTotal")
}

func TestUnescapeMount(t *testing.T) {
	assert.Equal(t, "/mnt/a b", unescapeMount(`/mnt/a\040b`))
	assert.Equal(t, `/mnt/a\0`, unescapeMount(`/mnt/a\0`), "truncated escape kept")
	assert.Equal(t, `/mnt/a\999b`, unescapeMount(`/mnt/a\999b`), "invalid escape kept")
}

func TestRegistered(t *testing.T) {
	c := &Collector{}
	assert.Equal(t, Name, c.Name())
	assert.True(t, c.Applicable(nil, nil))
}
