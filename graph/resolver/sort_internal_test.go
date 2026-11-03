package resolver

import (
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestSortHosts_Keys(t *testing.T) {
	tests := []struct {
		name string
		by   graph.HostSortField
		desc bool
		want []string
	}{
		{name: "IP by value, IPv4 first, none first", by: graph.HostSortFieldIP, want: []string{"none", "b", "a", "c", "d"}},
		{name: "IP descending", by: graph.HostSortFieldIP, desc: true, want: []string{"d", "c", "a", "b", "none"}},
		{name: "OS decoded once", by: graph.HostSortFieldOs, want: []string{"none", "a", "c", "d", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := &model.Inventory{}
			host := func(name, ip, os string) {
				h := model.Host{Base: model.Base{ID: uuid.New()}, Hostname: name, OS: json.RawMessage(os)}
				inv.Hosts = append(inv.Hosts, h)
				if ip != "" {
					inv.IPs = append(inv.IPs, model.HostIP{HostID: h.ID, IP: model.IP{Addr: netip.MustParseAddr(ip)}})
				}
			}
			host("a", "10.0.1.10", `{"pretty":"Debian 12"}`)
			host("b", "10.0.1.9", `{"id":"ubuntu"}`)
			host("c", "fd00::1", `{"pretty":"Debian 13"}`)
			host("d", "fd00::a", `{"pretty":"Rocky 9"}`)
			host("none", "", `{}`)
			x := newIndex(inv)
			hosts := append([]model.Host(nil), inv.Hosts...)
			x.sortHosts(hosts, &graph.HostSort{By: tt.by, Desc: &tt.desc}, inv.Hosts[0].LastSeenAt)
			var got []string
			for _, h := range hosts {
				got = append(got, h.Hostname)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSortApps_Version(t *testing.T) {
	host := model.Host{Base: model.Base{ID: uuid.New()}, Hostname: "web"}
	inv := &model.Inventory{Hosts: []model.Host{host}}
	for _, v := range []string{"v1.10.0", "v1.9.0", ""} {
		inv.Apps = append(inv.Apps, model.App{Base: model.Base{ID: uuid.New()}, HostID: host.ID, Name: "app" + v,
			Code: json.RawMessage(`{"version":"` + v + `"}`)})
	}
	x := newIndex(inv)
	apps := append([]model.App(nil), inv.Apps...)
	x.sortApps(apps, &graph.AppSort{By: graph.AppSortFieldVersion})
	assert.Equal(t, []string{"app", "appv1.9.0", "appv1.10.0"}, []string{apps[0].Name, apps[1].Name, apps[2].Name})
}
