package resolver

import (
	"cmp"
	"encoding/json"
	"net/netip"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/graph/union"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
)

// natural compares two texts with their numbers by value, so that web-2
// comes before web-10 and 8.10 after 8.9.
func natural(a, b string) int {
	for a != "" && b != "" {
		ca, cb := rune(a[0]), rune(b[0])
		if unicode.IsDigit(ca) && unicode.IsDigit(cb) {
			na, ra := leadingDigits(a)
			nb, rb := leadingDigits(b)
			na, nb = strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
			if c := cmp.Or(cmp.Compare(len(na), len(nb)), strings.Compare(na, nb)); c != 0 {
				return c
			}
			a, b = ra, rb
			continue
		}
		if c := cmp.Compare(unicode.ToLower(ca), unicode.ToLower(cb)); c != 0 {
			return c
		}
		a, b = a[1:], b[1:]
	}
	return cmp.Compare(len(a), len(b))
}

func leadingDigits(s string) (string, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i], s[i:]
}

func (x *index) projectSlug(h *model.Host) string {
	if h.ProjectID == nil || x.projects[*h.ProjectID] == nil {
		return ""
	}
	return x.projects[*h.ProjectID].Slug
}

func (x *index) envSlug(h *model.Host) string {
	if h.EnvironmentID == nil || x.envs[*h.EnvironmentID] == nil {
		return ""
	}
	return x.envs[*h.EnvironmentID].Slug
}

// mainIP is the address of the default route, else the first; not valid
// for a host without IP.
func (x *index) mainIP(h *model.Host) netip.Addr {
	ips := x.ips[h.ID]
	for _, ip := range ips {
		if ip.DefaultRoute {
			return ip.IP.Addr
		}
	}
	if len(ips) > 0 {
		return ips[0].IP.Addr
	}
	return netip.Addr{}
}

func osName(h *model.Host) string {
	var os struct {
		Pretty string `json:"pretty"`
		ID     string `json:"id"`
	}
	_ = json.Unmarshal(h.OS, &os)
	return cmp.Or(os.Pretty, os.ID)
}

// hostText is what a search of the hosts looks in: the hostname, the IPs
// and the versions of what the host runs.
func (x *index) hostText(h *model.Host) []string {
	out := []string{h.Hostname, osName(h)}
	for _, ip := range x.ips[h.ID] {
		out = append(out, ip.IP.String())
	}
	for _, s := range x.servicesOf[h.ID] {
		out = append(out, s.Type+" "+deref(s.Version))
	}
	for _, w := range x.webserversOf[h.ID] {
		out = append(out, w.Type+" "+deref(w.Version))
	}
	for _, a := range x.appsOf[h.ID] {
		out = append(out, a.Name, codeVersion(&a))
	}
	return out
}

func codeVersion(a *model.App) string {
	var c struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(a.Code, &c)
	return c.Version
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func direction(desc *bool, c int) int {
	if desc != nil && *desc {
		return -c
	}
	return c
}

// sortKey is the value a sort compares, computed once per element: only
// the field of the sort is set.
type sortKey struct {
	text string
	num  int
	ip   netip.Addr
	at   time.Time
}

func (a sortKey) compare(b sortKey) int {
	return cmp.Or(natural(a.text, b.text), cmp.Compare(a.num, b.num), a.ip.Compare(b.ip), a.at.Compare(b.at))
}

// The sorts order by the field, then by name ascending whatever the
// direction.
func (x *index) sortHosts(hosts []model.Host, s *graph.HostSort, now time.Time) {
	by := graph.HostSortFieldHostname
	var desc *bool
	if s != nil {
		by, desc = s.By, s.Desc
	}
	keys := make(map[uuid.UUID]sortKey, len(hosts))
	for i := range hosts {
		h := &hosts[i]
		var k sortKey
		switch by {
		case graph.HostSortFieldHostname:
			k.text = h.Hostname
		case graph.HostSortFieldProject:
			k.text = x.projectSlug(h)
		case graph.HostSortFieldEnv:
			k.text = x.envSlug(h)
		case graph.HostSortFieldIP:
			k.ip = x.mainIP(h)
		case graph.HostSortFieldOs:
			k.text = osName(h)
		case graph.HostSortFieldApps:
			k.num = len(x.appsOf[h.ID])
		case graph.HostSortFieldStatus:
			k.num = slices.Index(freshness, status(&x.inv.Namespace, h, now))
		case graph.HostSortFieldLastSeen:
			k.at = h.LastSeenAt
		}
		keys[h.ID] = k
	}
	slices.SortStableFunc(hosts, func(a, b model.Host) int {
		return cmp.Or(direction(desc, keys[a.ID].compare(keys[b.ID])), natural(a.Hostname, b.Hostname))
	})
}

func (x *index) sortApps(apps []model.App, s *graph.AppSort) {
	by := graph.AppSortFieldName
	var desc *bool
	if s != nil {
		by, desc = s.By, s.Desc
	}
	keys := make(map[uuid.UUID]sortKey, len(apps))
	for i := range apps {
		a := &apps[i]
		var k sortKey
		switch by {
		case graph.AppSortFieldName:
			k.text = a.Name
		case graph.AppSortFieldProject:
			k.text = x.appProject(a)
		case graph.AppSortFieldEnv:
			k.text = x.appEnv(a)
		case graph.AppSortFieldHostname:
			k.text = x.hosts[a.HostID].Hostname
		case graph.AppSortFieldVersion:
			k.text = codeVersion(a)
		case graph.AppSortFieldFramework:
			k.text = deref(a.Framework)
		}
		keys[a.ID] = k
	}
	slices.SortStableFunc(apps, func(a, b model.App) int {
		ha, hb := x.hosts[a.HostID], x.hosts[b.HostID]
		return cmp.Or(direction(desc, keys[a.ID].compare(keys[b.ID])), natural(a.Name, b.Name), natural(ha.Hostname, hb.Hostname))
	})
}

func (x *index) appProject(a *model.App) string {
	if a.ProjectID != nil && x.projects[*a.ProjectID] != nil {
		return x.projects[*a.ProjectID].Slug
	}
	return x.projectSlug(x.hosts[a.HostID])
}

func (x *index) appEnv(a *model.App) string {
	if a.EnvironmentID != nil && x.envs[*a.EnvironmentID] != nil {
		return x.envs[*a.EnvironmentID].Slug
	}
	return x.envSlug(x.hosts[a.HostID])
}

func (x *index) sortServices(services []model.Service, s *graph.ServiceSort) {
	by := graph.ServiceSortFieldType
	var desc *bool
	if s != nil {
		by, desc = s.By, s.Desc
	}
	slices.SortStableFunc(services, func(a, b model.Service) int {
		ha, hb := x.hosts[a.HostID], x.hosts[b.HostID]
		var c int
		switch by {
		case graph.ServiceSortFieldType:
			c = natural(a.Type, b.Type)
		case graph.ServiceSortFieldVersion:
			c = natural(deref(a.Version), deref(b.Version))
		case graph.ServiceSortFieldHostname:
			c = natural(ha.Hostname, hb.Hostname)
		}
		return cmp.Or(direction(desc, c), natural(a.Type, b.Type), natural(ha.Hostname, hb.Hostname), natural(a.Instance, b.Instance))
	})
}

// componentKey is what a component sorts on.
type componentKey struct {
	typ, instance, version string
	host                   *model.Host
}

func (x *index) componentKey(c union.Component) componentKey {
	switch c := c.(type) {
	case *model.Service:
		return componentKey{typ: c.Type, instance: c.Instance, version: deref(c.Version), host: x.hosts[c.HostID]}
	case *model.Webserver:
		return componentKey{typ: c.Type, version: deref(c.Version), host: x.hosts[c.HostID]}
	}
	return componentKey{host: &model.Host{}}
}

// sortComponents sorts services and webservers as sortServices does.
func (x *index) sortComponents(components []union.Component, s *graph.ServiceSort) {
	by := graph.ServiceSortFieldType
	var desc *bool
	if s != nil {
		by, desc = s.By, s.Desc
	}
	slices.SortStableFunc(components, func(ca, cb union.Component) int {
		a, b := x.componentKey(ca), x.componentKey(cb)
		var c int
		switch by {
		case graph.ServiceSortFieldType:
			c = natural(a.typ, b.typ)
		case graph.ServiceSortFieldVersion:
			c = natural(a.version, b.version)
		case graph.ServiceSortFieldHostname:
			c = natural(a.host.Hostname, b.host.Hostname)
		}
		return cmp.Or(direction(desc, c), natural(a.typ, b.typ), natural(a.host.Hostname, b.host.Hostname), natural(a.instance, b.instance))
	})
}
