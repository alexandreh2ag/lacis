package resolver

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/model"
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

// mainIP is the address of the default route, else the first.
func (x *index) mainIP(h *model.Host) string {
	ips := x.ips[h.ID]
	for _, ip := range ips {
		if ip.DefaultRoute {
			return ip.IP.String()
		}
	}
	if len(ips) > 0 {
		return ips[0].IP.String()
	}
	return ""
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

// The sorts order by the field, then by name ascending whatever the
// direction.
func (x *index) sortHosts(hosts []model.Host, s *graph.HostSort, now time.Time) {
	by := graph.HostSortFieldHostname
	var desc *bool
	if s != nil {
		by, desc = s.By, s.Desc
	}
	rank := func(h *model.Host) int { return slices.Index(freshness, status(&x.inv.Namespace, h, now)) }
	slices.SortStableFunc(hosts, func(a, b model.Host) int {
		var c int
		switch by {
		case graph.HostSortFieldHostname:
			c = natural(a.Hostname, b.Hostname)
		case graph.HostSortFieldProject:
			c = natural(x.projectSlug(&a), x.projectSlug(&b))
		case graph.HostSortFieldEnv:
			c = natural(x.envSlug(&a), x.envSlug(&b))
		case graph.HostSortFieldIP:
			c = natural(x.mainIP(&a), x.mainIP(&b))
		case graph.HostSortFieldOs:
			c = natural(osName(&a), osName(&b))
		case graph.HostSortFieldApps:
			c = cmp.Compare(len(x.appsOf[a.ID]), len(x.appsOf[b.ID]))
		case graph.HostSortFieldStatus:
			c = cmp.Compare(rank(&a), rank(&b))
		case graph.HostSortFieldLastSeen:
			c = a.LastSeenAt.Compare(b.LastSeenAt)
		}
		return cmp.Or(direction(desc, c), natural(a.Hostname, b.Hostname))
	})
}

func (x *index) sortApps(apps []model.App, s *graph.AppSort) {
	by := graph.AppSortFieldName
	var desc *bool
	if s != nil {
		by, desc = s.By, s.Desc
	}
	slices.SortStableFunc(apps, func(a, b model.App) int {
		ha, hb := x.hosts[a.HostID], x.hosts[b.HostID]
		var c int
		switch by {
		case graph.AppSortFieldName:
			c = natural(a.Name, b.Name)
		case graph.AppSortFieldProject:
			c = natural(x.appProject(&a), x.appProject(&b))
		case graph.AppSortFieldEnv:
			c = natural(x.appEnv(&a), x.appEnv(&b))
		case graph.AppSortFieldHostname:
			c = natural(ha.Hostname, hb.Hostname)
		case graph.AppSortFieldVersion:
			c = natural(codeVersion(&a), codeVersion(&b))
		case graph.AppSortFieldFramework:
			c = natural(deref(a.Framework), deref(b.Framework))
		}
		return cmp.Or(direction(desc, c), natural(a.Name, b.Name), natural(ha.Hostname, hb.Hostname))
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
