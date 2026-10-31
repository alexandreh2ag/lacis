package linker

import (
	"net/netip"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
)

// Networks gives the network of an IP: the one holding the most specific
// CIDR containing it, unless its host was moved by hand into a network
// split from that one.
type Networks struct {
	cidrs []model.NetworkCIDR
	moved map[uuid.UUID]map[uuid.UUID]uuid.UUID
}

func NewNetworks(inv *model.Inventory) *Networks {
	n := &Networks{cidrs: inv.CIDRs, moved: map[uuid.UUID]map[uuid.UUID]uuid.UUID{}}
	for _, hn := range inv.HostNetworks {
		if n.moved[hn.HostID] == nil {
			n.moved[hn.HostID] = map[uuid.UUID]uuid.UUID{}
		}
		n.moved[hn.HostID][hn.FromNetworkID] = hn.NetworkID
	}
	return n
}

// ByCIDR returns the network whose CIDR holds ip, the longest prefix first:
// never a network split from another, which holds no CIDR.
func (n *Networks) ByCIDR(ip netip.Addr) (uuid.UUID, bool) {
	best, bits := uuid.Nil, -1
	for _, c := range n.cidrs {
		if c.CIDR.Contains(ip) && c.CIDR.Bits() > bits {
			best, bits = c.NetworkID, c.CIDR.Bits()
		}
	}
	return best, bits >= 0
}

// Of returns the network of ip on the host.
func (n *Networks) Of(hostID uuid.UUID, ip netip.Addr) (uuid.UUID, bool) {
	network, ok := n.ByCIDR(ip)
	if !ok {
		return uuid.Nil, false
	}
	if moved, ok := n.moved[hostID][network]; ok {
		return moved, true
	}
	return network, true
}
