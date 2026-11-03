package service

import (
	"net/netip"
	"testing"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestIsPublic(t *testing.T) {
	for ip, want := range map[string]bool{
		"203.0.113.10": true, "52.95.155.20": true, "2001:db8::1": true,
		"10.0.1.20": false, "172.16.1.1": false, "192.168.1.1": false, "fd00::1": false,
		"127.0.0.1": false, "fe80::1": false, "0.0.0.0": false,
	} {
		assert.Equal(t, want, isPublic(netip.MustParseAddr(ip)), ip)
	}
}

func TestSlugOf(t *testing.T) {
	assert.Equal(t, "10-0-1-0-24", slugOf(netip.MustParsePrefix("10.0.1.0/24")))
	assert.Equal(t, "fd00-1-64", slugOf(netip.MustParsePrefix("fd00:1::/64")))
}

func TestEndpoints(t *testing.T) {
	n := &normalizer{host: &model.Host{Base: model.Base{ID: uuid.New()}, NamespaceID: uuid.New()}}
	owner := uuid.New()
	got := n.endpoints([]string{"0.0.0.0:5432", "[::]:9200", "[::ffff:10.0.0.1]:80", "unix:/run/php/x.sock", "unix:/run/php/x.sock", "9000", "db:3306x", ":80"}, model.EndpointOwnerService, owner)
	port := func(p int) *int { return &p }
	var short []model.Endpoint
	for _, e := range got {
		assert.Equal(t, owner, e.OwnerID)
		assert.Equal(t, n.host.ID, e.HostID)
		short = append(short, model.Endpoint{Kind: e.Kind, Address: e.Address, Port: e.Port})
	}
	assert.Equal(t, []model.Endpoint{
		{Kind: "tcp", Address: "0.0.0.0", Port: port(5432)},
		{Kind: "tcp", Address: "::", Port: port(9200)},
		{Kind: "tcp", Address: "::ffff:10.0.0.1", Port: port(80)},
		{Kind: "unix", Address: "/run/php/x.sock"},
	}, short, "once each; a bare port, a bad port or no host skipped")
}

func TestMarshalOrNil(t *testing.T) {
	assert.Nil(t, marshalOrNil(nil))
	var p *model.Host
	assert.Nil(t, marshalOrNil(p))
	assert.JSONEq(t, `{"a":1}`, string(marshalOrNil(map[string]int{"a": 1})))
	assert.Nil(t, marshalOrNil(func() {}), "not encodable")
	assert.Nil(t, optional(""))
	assert.Equal(t, 0, deref(nil))
}

func TestRuleDomains(t *testing.T) {
	for rule, want := range map[string][]string{
		"Host(`a.example`) || Host(`b.example`)":               {"a.example", "b.example"},
		"Host(`a.example`, `b.example`) && PathPrefix(`/api`)": {"a.example", "b.example"},
		"HostRegexp(`^.+\\.dev1\\.acme\\.example$`)":           {"~^.+\\.dev1\\.acme\\.example$"},
		"HostSNI(`*`)":               nil,
		"HostSNI(`db.acme.example`)": {"db.acme.example"},
		"PathPrefix(`/`)":            nil,
	} {
		assert.Equal(t, want, ruleDomains(rule), rule)
	}
}
