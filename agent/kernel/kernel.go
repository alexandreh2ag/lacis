// Package kernel gives the system values that are not files: network
// interfaces, disk sizes and name resolution. Collectors go through a Kernel, mocked
// in tests, like they go through ctx.Fs for files.
package kernel

import (
	"context"
	"net"
	"net/netip"
	"syscall"
)

type Kernel interface {
	// Interfaces returns the network interfaces with their addresses.
	Interfaces() ([]Interface, error)
	// DiskSize returns the size in bytes of the file system mounted on
	// path.
	DiskSize(path string) (uint64, error)
	// LookupHost resolves a name with the resolver of the host, as the apps
	// of the host would.
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type Interface struct {
	Name     string
	Loopback bool
	Up       bool
	// Addrs are the addresses with the prefix of their subnet.
	Addrs []netip.Prefix
}

// OS is the Kernel of the running system.
type OS struct{}

var _ Kernel = OS{}

func New() Kernel {
	return OS{}
}

func (OS) Interfaces() ([]Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]Interface, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		i := Interface{
			Name:     iface.Name,
			Loopback: iface.Flags&net.FlagLoopback != 0,
			Up:       iface.Flags&net.FlagUp != 0,
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			addr, ok := netip.AddrFromSlice(ipNet.IP)
			if !ok {
				continue
			}
			ones, _ := ipNet.Mask.Size()
			i.Addrs = append(i.Addrs, netip.PrefixFrom(addr.Unmap(), ones))
		}
		out = append(out, i)
	}
	return out, nil
}

func (OS) DiskSize(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Blocks * uint64(st.Bsize), nil //nolint:gosec // block sizes are positive
}

func (OS) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}
