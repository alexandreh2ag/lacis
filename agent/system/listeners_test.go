package system

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tcp = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:18EB 00000000:0000 0A 00000000:00000000 00:00000000 00000000   112        0 22001 1 0000000000000000 100 0 0 10 0
   1: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 22002 1 0000000000000000 100 0 0 10 0
   2: 0C01000A:0050 1401000A:C350 01 00000000:00000000 00:00000000 00000000     0        0 22003 1 0000000000000000 100 0 0 10 0
`

const tcp6 = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000000000000:0050 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 22004 1 0000000000000000 100 0 0 10 0
   1: 0000000000000000FFFF00000100007F:1538 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 22005 1 0000000000000000 100 0 0 10 0
`

const unix = `Num       RefCount Protocol Flags    Type St Inode Path
0000000000000000: 00000002 00000000 00010000 0001 01 22010 /run/php/php8.2-fpm.sock
0000000000000000: 00000002 00000000 00010000 0001 01 22011 @/tmp/.X11-unix/X0
0000000000000000: 00000003 00000000 00000000 0001 03 22012 /run/systemd/journal/stdout
0000000000000000: 00000002 00000000 00010000 0001 01 22013
`

func TestListeners(t *testing.T) {
	sys, f, _ := newSystem(t, FamilyDebian, map[string]string{
		TCPFile:          tcp,
		TCP6File:         tcp6,
		UnixFile:         unix,
		"/proc/812/fd/6": "",
		"/proc/812/fd/7": "",
		"/proc/812/fd/8": "",
		"/proc/900/fd/3": "",
		"/proc/901/fd/3": "",
		"/proc/77/fd/9":  "",
	})
	f.Link("/proc/812/fd/6", "socket:[22002]")
	f.Link("/proc/812/fd/7", "socket:[22004]")
	f.Link("/proc/812/fd/8", "/var/log/nginx/access.log")
	f.Link("/proc/900/fd/3", "socket:[22001]")
	f.Link("/proc/901/fd/3", "socket:[22001]") // forked worker sharing it
	f.Link("/proc/77/fd/9", "socket:[22010]")

	got, err := sys.Listeners()
	require.NoError(t, err)
	assert.Equal(t, []Listener{
		{Proto: ProtoTCP, Address: "0.0.0.0:80", PID: 812, Inode: 22002},
		{Proto: ProtoTCP, Address: "127.0.0.1:5432", PID: 0, Inode: 22005},
		{Proto: ProtoTCP, Address: "127.0.0.1:6379", PID: 900, Inode: 22001},
		{Proto: ProtoTCP, Address: "[::]:80", PID: 812, Inode: 22004},
		{Proto: ProtoUnix, Address: "/run/php/php8.2-fpm.sock", PID: 77, Inode: 22010},
		{Proto: ProtoUnix, Address: "@/tmp/.X11-unix/X0", PID: 0, Inode: 22011},
	}, got)
}

func TestListeners_NoIPv6(t *testing.T) {
	sys, _, _ := newSystem(t, FamilyDebian, map[string]string{TCPFile: tcp, UnixFile: unix, "/proc/1/status": ""})
	got, err := sys.Listeners()
	require.NoError(t, err)
	assert.Len(t, got, 4)
}

func TestListeners_Errors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "no tcp", files: map[string]string{}, want: TCPFile},
		{name: "bad address", files: map[string]string{TCPFile: "h\n 0: 0100:18EB 0:0 0A 0 0 0 0 0 1\n"}, want: `address "0100:18EB"`},
		{name: "bad port", files: map[string]string{TCPFile: "h\n 0: 0100007F:ZZ 0:0 0A 0 0 0 0 0 1\n"}, want: `port "0100007F:ZZ"`},
		{name: "bad tcp inode", files: map[string]string{TCPFile: "h\n 0: 0100007F:0050 0:0 0A 0 0 0 0 0 x\n"}, want: `inode "x"`},
		{name: "no unix", files: map[string]string{TCPFile: tcp, TCP6File: tcp6}, want: UnixFile},
		{name: "bad unix inode", files: map[string]string{TCPFile: tcp, UnixFile: "h\n0: 2 0 00010000 0001 01 x /run/a.sock\n"}, want: `inode "x"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys, _, _ := newSystem(t, FamilyDebian, tt.files)
			_, err := sys.Listeners()
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestParseHexAddrPort(t *testing.T) {
	got, err := parseHexAddrPort("B80D0120000000000000000001000000:01BB")
	require.NoError(t, err)
	assert.Equal(t, netip.MustParseAddrPort("[2001:db8::1]:443"), got)
}
