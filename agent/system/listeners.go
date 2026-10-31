package system

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/afero"
)

const (
	TCPFile  = "/proc/net/tcp"
	TCP6File = "/proc/net/tcp6"
	UnixFile = "/proc/net/unix"
)

const (
	ProtoTCP  = "tcp"
	ProtoUnix = "unix"

	tcpListen = "0A"
	// unixAcceptCon is __SO_ACCEPTCON: a listening unix socket.
	unixAcceptCon = 0x10000
)

// Listener is a listening TCP or unix socket.
type Listener struct {
	Proto string
	// Address is "ip:port" ("[::]:80" for IPv6) or the socket path; an
	// abstract unix socket starts with "@".
	Address string
	// PID is the process holding the socket, 0 when unknown.
	PID   int
	Inode uint64
}

// Listeners returns the listening sockets with their process, sorted by
// protocol and address.
func (s *System) Listeners() ([]Listener, error) {
	return s.listeners.get(func() ([]Listener, error) {
		var out []Listener
		for _, path := range []string{TCPFile, TCP6File} {
			l, err := s.tcpListeners(path)
			if err != nil {
				return nil, err
			}
			out = append(out, l...)
		}
		l, err := s.unixListeners()
		if err != nil {
			return nil, err
		}
		out = append(out, l...)

		owners, err := s.socketOwners()
		if err != nil {
			return nil, err
		}
		for i := range out {
			out[i].PID = owners[out[i].Inode]
		}
		slices.SortFunc(out, func(a, b Listener) int {
			if c := strings.Compare(a.Proto, b.Proto); c != 0 {
				return c
			}
			return strings.Compare(a.Address, b.Address)
		})
		return out, nil
	})
}

// tcpListeners parses /proc/net/tcp or tcp6:
// sl local_address rem_address st tx_queue:rx_queue tr:tm->when retrnsmt uid timeout inode
func (s *System) tcpListeners(path string) ([]Listener, error) {
	var out []Listener
	err := s.scanTable(path, func(fields []string) error {
		if len(fields) < 10 || fields[3] != tcpListen {
			return nil
		}
		addr, err := parseHexAddrPort(fields[1])
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			return fmt.Errorf("%s: inode %q: %w", path, fields[9], err)
		}
		out = append(out, Listener{Proto: ProtoTCP, Address: addr.String(), Inode: inode})
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) && path == TCP6File {
		return nil, nil // IPv6 disabled
	}
	return out, err
}

// parseHexAddrPort decodes "0100007F:1F90": the address as 32-bit words in
// host order (little-endian), the port in big-endian.
func parseHexAddrPort(s string) (netip.AddrPort, error) {
	hexAddr, hexPort, ok := strings.Cut(s, ":")
	b, err := hex.DecodeString(hexAddr)
	if !ok || err != nil || (len(b) != 4 && len(b) != 16) {
		return netip.AddrPort{}, fmt.Errorf("address %q", s)
	}
	port, err := strconv.ParseUint(hexPort, 16, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("port %q", s)
	}
	for i := 0; i < len(b); i += 4 {
		binary.BigEndian.PutUint32(b[i:], binary.LittleEndian.Uint32(b[i:]))
	}
	addr, _ := netip.AddrFromSlice(b)
	return netip.AddrPortFrom(addr.Unmap(), uint16(port)), nil
}

// unixListeners parses /proc/net/unix:
// Num RefCount Protocol Flags Type St Inode Path
func (s *System) unixListeners() ([]Listener, error) {
	var out []Listener
	err := s.scanTable(UnixFile, func(fields []string) error {
		if len(fields) < 8 {
			return nil // no path: an unnamed socket
		}
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil || flags&unixAcceptCon == 0 {
			return nil
		}
		inode, err := strconv.ParseUint(fields[6], 10, 64)
		if err != nil {
			return fmt.Errorf("%s: inode %q: %w", UnixFile, fields[6], err)
		}
		out = append(out, Listener{Proto: ProtoUnix, Address: fields[7], Inode: inode})
		return nil
	})
	return out, err
}

// scanTable calls fn on the fields of each line of a /proc/net table, its
// header skipped.
func (s *System) scanTable(path string, fn func(fields []string) error) error {
	f, err := s.ctx.Fs.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Scan()
	for scanner.Scan() {
		if err := fn(strings.Fields(scanner.Text())); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// socketOwners maps the socket inodes to the PID holding them, read from
// the /proc/<pid>/fd links. The lowest PID wins when several share one.
func (s *System) socketOwners() (map[uint64]int, error) {
	pids, err := s.pids()
	if err != nil {
		return nil, err
	}
	owners := map[uint64]int{}
	for _, pid := range pids {
		dir := fmt.Sprintf("%s/%d/fd", ProcDir, pid)
		fds, err := afero.ReadDir(s.ctx.Fs, dir)
		if err != nil {
			continue // gone, or not ours to read
		}
		for _, fd := range fds {
			target, err := s.Readlink(dir + "/" + fd.Name())
			if err != nil {
				continue
			}
			inode, ok := strings.CutPrefix(target, "socket:[")
			if !ok {
				continue
			}
			if n, err := strconv.ParseUint(strings.TrimSuffix(inode, "]"), 10, 64); err == nil {
				if _, seen := owners[n]; !seen {
					owners[n] = pid
				}
			}
		}
	}
	return owners, nil
}
