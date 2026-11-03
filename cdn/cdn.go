// Package cdn holds the IP ranges of the CDNs Lacis knows, by name, so that
// linking.cdns names them instead of listing their ranges.
package cdn

import (
	"maps"
	"net/netip"
	"slices"
)

// Custom names the ranges of linking.cdn_ranges, a CDN without a name.
const Custom = "cdn"

// CDN is a CDN and its IP ranges.
type CDN struct {
	Name   string
	Ranges []netip.Prefix
}

// Contains tells whether ip is in one of the ranges.
func (c CDN) Contains(ip netip.Addr) bool {
	for _, p := range c.Ranges {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// known are the ranges each CDN publishes; check them against the source
// before a release.
var known = map[string][]string{
	// https://www.cloudflare.com/ips/
	"cloudflare": {
		"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18",
		"108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17",
		"162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
		"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32",
		"2a06:98c0::/29", "2c0f:f248::/32",
	},
}

// Names are the CDNs known, in order.
func Names() []string {
	return slices.Sorted(maps.Keys(known))
}

// Known returns the CDN of that name, and whether it is known.
func Known(name string) (CDN, bool) {
	ranges, ok := known[name]
	if !ok {
		return CDN{}, false
	}
	c := CDN{Name: name}
	for _, r := range ranges {
		c.Ranges = append(c.Ranges, netip.MustParsePrefix(r))
	}
	return c, true
}
