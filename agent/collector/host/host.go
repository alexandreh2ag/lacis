// Package host collects the description of the machine: OS, kernel,
// architecture, virtualization, CPU, memory, disks and interfaces.
package host

import (
	"bufio"
	stdContext "context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/collector"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

const Name = "host"

// Files and commands read by the collector.
const (
	KernelReleaseFile = "/proc/sys/kernel/osrelease"
	// ArchFile exists since Linux 6.1; older kernels fall back on the
	// architecture of the agent binary.
	ArchFile          = "/proc/sys/kernel/arch"
	CPUInfoFile       = "/proc/cpuinfo"
	MemInfoFile       = "/proc/meminfo"
	MountsFile        = "/proc/mounts"
	RouteFile         = "/proc/net/route"
	IPv6RouteFile     = "/proc/net/ipv6_route"
	CgroupCPUMax      = "/sys/fs/cgroup/cpu.max"
	CgroupMemoryMax   = "/sys/fs/cgroup/memory.max"
	CgroupV1CPUQuota  = "/sys/fs/cgroup/cpu/cpu.cfs_quota_us"
	CgroupV1CPUPeriod = "/sys/fs/cgroup/cpu/cpu.cfs_period_us"
	CgroupV1Memory    = "/sys/fs/cgroup/memory/memory.limit_in_bytes"
	DetectVirtCommand = "/usr/bin/systemd-detect-virt"
	DockerEnvFile     = "/.dockerenv"
	ContainerEnvFile  = "/run/.containerenv"
)

// DiskFsTypes are the file systems reported as disks; the others are
// virtual (proc, tmpfs, overlay…).
var DiskFsTypes = []string{"ext2", "ext3", "ext4", "xfs", "btrfs", "zfs", "f2fs", "jfs", "reiserfs"}

// goArchToMachine maps the architecture of the agent binary to uname -m.
var goArchToMachine = map[string]string{"amd64": "x86_64", "arm64": "aarch64", "386": "i686", "arm": "armv7l"}

const gib = 1 << 30

func init() {
	collector.Register(Name, func(*context.Context) (collector.Collector, error) { return &Collector{}, nil })
}

type Collector struct{}

func (*Collector) Name() string { return Name }

// Applicable is always true: every host describes itself.
func (*Collector) Applicable(*context.Context, *system.System) bool { return true }

// Collect fills out.Host, except the hostname and the labels. A part that
// cannot be read is left empty and reported, the others are still filled.
func (*Collector) Collect(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, out *snapshot.Snapshot) error {
	h := &out.Host
	h.OS = sys.OS
	var errs []error
	report := func(what string, err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", what, err))
		}
	}

	kernel, err := readTrimmed(ctx.Fs, KernelReleaseFile)
	report("kernel", err)
	h.Kernel = kernel
	h.Arch = arch(ctx.Fs)
	h.Virtualization = virtualization(stdCtx, ctx)

	h.CPU, err = cpu(ctx.Fs)
	report("cpu", err)
	h.MemoryMB, err = memory(ctx.Fs)
	report("memory", err)
	h.Disks, err = disks(ctx)
	report("disks", err)
	h.Interfaces, err = interfaces(ctx)
	report("interfaces", err)
	return errors.Join(errs...)
}

func readTrimmed(fsys afero.Fs, path string) (string, error) {
	b, err := afero.ReadFile(fsys, path)
	return strings.TrimSpace(string(b)), err
}

func exists(fsys afero.Fs, path string) bool {
	_, err := fsys.Stat(path)
	return err == nil
}

func arch(fsys afero.Fs) string {
	if a, err := readTrimmed(fsys, ArchFile); err == nil && a != "" {
		return a
	}
	if m, ok := goArchToMachine[runtime.GOARCH]; ok {
		return m
	}
	return runtime.GOARCH
}

// virtualization asks systemd-detect-virt ("none" on bare metal), and
// otherwise recognizes Docker and Podman containers; "" when unknown.
func virtualization(stdCtx stdContext.Context, ctx *context.Context) string {
	if exists(ctx.Fs, DetectVirtCommand) {
		// Exits with 1 when there is no virtualization, still printing "none".
		out, _ := ctx.Executor.Run(stdCtx, DetectVirtCommand)
		if v := strings.TrimSpace(string(out)); v != "" {
			return v
		}
	}
	switch {
	case exists(ctx.Fs, DockerEnvFile):
		return "docker"
	case exists(ctx.Fs, ContainerEnvFile):
		return "podman"
	}
	return ""
}

// cpu counts the visible processors, and the effective ones when a cgroup
// quota is lower.
func cpu(fsys afero.Fs) (*snapshot.CPU, error) {
	f, err := fsys.Open(CPUInfoFile)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	c := &snapshot.CPU{Source: snapshot.ResourceSourceProc}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "processor":
			c.VisibleCores++
		case "model name":
			if c.Model == "" {
				c.Model = strings.TrimSpace(value)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	c.EffectiveCores = c.VisibleCores
	if quota, ok := cgroupCPUs(fsys); ok && quota < c.VisibleCores {
		c.EffectiveCores = math.Round(quota*100) / 100
		c.Source = snapshot.ResourceSourceCgroup
	}
	return c, nil
}

// cgroupCPUs returns the CPU quota of the cgroup in cores, cgroup v2 first.
func cgroupCPUs(fsys afero.Fs) (float64, bool) {
	if s, err := readTrimmed(fsys, CgroupCPUMax); err == nil {
		quota, period, ok := strings.Cut(s, " ")
		return ratio(quota, period, ok)
	}
	quota, err1 := readTrimmed(fsys, CgroupV1CPUQuota)
	period, err2 := readTrimmed(fsys, CgroupV1CPUPeriod)
	return ratio(quota, period, err1 == nil && err2 == nil)
}

func ratio(quota, period string, ok bool) (float64, bool) {
	q, err1 := strconv.ParseFloat(quota, 64)
	p, err2 := strconv.ParseFloat(period, 64)
	if !ok || err1 != nil || err2 != nil || q <= 0 || p <= 0 {
		return 0, false // "max" or -1: no quota
	}
	return q / p, true
}

// memory reads MemTotal, and the cgroup limit when lower.
func memory(fsys afero.Fs) (*snapshot.Memory, error) {
	f, err := fsys.Open(MemInfoFile)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var totalKB int64 = -1
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			totalKB, err = strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("MemTotal: %w", err)
			}
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if totalKB < 0 {
		return nil, errors.New("MemTotal not found")
	}
	m := &snapshot.Memory{Visible: totalKB / 1024, Source: snapshot.ResourceSourceProc}
	m.Effective = m.Visible
	for _, path := range []string{CgroupMemoryMax, CgroupV1Memory} {
		s, err := readTrimmed(fsys, path)
		if err != nil {
			continue
		}
		// "max" (v2) and huge values (v1) mean no limit.
		if limit, err := strconv.ParseInt(s, 10, 64); err == nil && limit/(1<<20) < m.Visible {
			m.Effective = limit / (1 << 20)
			m.Source = snapshot.ResourceSourceCgroup
		}
		break
	}
	return m, nil
}

// disks lists the mounted disk file systems, once per device.
func disks(ctx *context.Context) ([]snapshot.Disk, error) {
	f, err := ctx.Fs.Open(MountsFile)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []snapshot.Disk
	var errs []error
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 || !slices.Contains(DiskFsTypes, fields[2]) || seen[fields[0]] {
			continue
		}
		seen[fields[0]] = true
		mount := unescapeMount(fields[1])
		size, err := ctx.Kernel.DiskSize(mount)
		if err != nil {
			errs = append(errs, fmt.Errorf("statfs %s: %w", mount, err))
			continue
		}
		out = append(out, snapshot.Disk{Mount: mount, SizeGB: int64(size / gib)}) //nolint:gosec // sizes fit
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b snapshot.Disk) int { return strings.Compare(a.Mount, b.Mount) })
	return out, errors.Join(errs...)
}

// unescapeMount decodes the octal escapes of /proc/mounts, e.g. \040.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// interfaces lists the interfaces that are up with an address, loopback
// excluded, sorted by name.
func interfaces(ctx *context.Context) ([]snapshot.Interface, error) {
	ifaces, err := ctx.Kernel.Interfaces()
	if err != nil {
		return nil, err
	}
	defaults, err := defaultRouteInterfaces(ctx.Fs)
	if err != nil {
		return nil, err
	}
	var out []snapshot.Interface
	for _, iface := range ifaces {
		if iface.Loopback || !iface.Up {
			continue
		}
		i := snapshot.Interface{Name: iface.Name, DefaultRoute: defaults[iface.Name]}
		for _, p := range iface.Addrs {
			if p.Addr().IsLinkLocalUnicast() {
				continue
			}
			i.IPs = append(i.IPs, p.String())
		}
		if len(i.IPs) > 0 {
			out = append(out, i)
		}
	}
	slices.SortFunc(out, func(a, b snapshot.Interface) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// defaultRouteInterfaces returns the interfaces carrying an IPv4 or IPv6
// default route.
func defaultRouteInterfaces(fsys afero.Fs) (map[string]bool, error) {
	out := map[string]bool{}
	// Iface Destination Gateway Flags … with a header line.
	err := scanFile(fsys, RouteFile, func(fields []string) {
		if len(fields) >= 2 && fields[1] == "00000000" {
			out[fields[0]] = true
		}
	})
	if err != nil {
		return nil, err
	}
	// Destination PrefixLen Source SrcPrefixLen NextHop Metric RefCnt Use
	// Flags Iface, without header.
	err = scanFile(fsys, IPv6RouteFile, func(fields []string) {
		if len(fields) >= 10 && fields[0] == strings.Repeat("0", 32) && fields[1] == "00" && fields[9] != "lo" {
			out[fields[9]] = true
		}
	})
	if errors.Is(err, fs.ErrNotExist) {
		err = nil // IPv6 disabled
	}
	return out, err
}

func scanFile(fsys afero.Fs, path string, fn func(fields []string)) error {
	f, err := fsys.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fn(strings.Fields(scanner.Text()))
	}
	return scanner.Err()
}
