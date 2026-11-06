package resolver

import (
	"cmp"
	"slices"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/placement"
	"github.com/google/uuid"
)

// hostPlace is the place of the labels of a host.
func (x *index) hostPlace(hostID uuid.UUID) placement.Place {
	var p placement.Place
	if h := x.hosts[hostID]; h != nil {
		p.Project, p.Env = deref(h.ProjectID), deref(h.EnvironmentID)
	}
	return p
}

// appsOfProject are the apps of a project, by their own project or, without
// one, by that of their host.
func (x *index) appsOfProject(projectID uuid.UUID) []*model.App {
	var out []*model.App
	for i := range x.inv.Apps {
		a := &x.inv.Apps[i]
		if placement.Of(a, x.hostPlace(a.HostID)).Project == projectID {
			out = append(out, a)
		}
	}
	return out
}

// projectEnvs are the envs of the apps and of the hosts of a project, by
// their order.
func (x *index) projectEnvs(projectID uuid.UUID) []*model.Environment {
	var out []*model.Environment
	add := func(envID uuid.UUID) {
		if e := x.envs[envID]; e != nil && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	for _, a := range x.appsOfProject(projectID) {
		add(placement.Of(a, x.hostPlace(a.HostID)).Env)
	}
	for _, h := range x.inv.Hosts {
		for _, p := range x.places.Of(h.ID) {
			if p.Project == projectID {
				add(p.Env)
			}
		}
	}
	slices.SortFunc(out, func(a, b *model.Environment) int {
		return cmp.Or(cmp.Compare(a.SortOrder, b.SortOrder), cmp.Compare(a.Slug, b.Slug))
	})
	return out
}

// projectUses are the other projects the apps of a project use at the end
// of their effective links, by slug, and whether they reach an external
// target. An app ends in its own project, a service, a webserver or a host
// in that of the labels of its host.
func (x *index) projectUses(projectID uuid.UUID) ([]*model.Project, bool) {
	var deps []*model.Project
	externals := false
	for _, a := range x.appsOfProject(projectID) {
		for _, p := range x.paths.Uses(a.ID) {
			last := p.Last()
			if last.ToID == nil {
				externals = true
				continue
			}
			var to uuid.UUID
			switch last.ToType {
			case model.LinkNodeApp:
				if b := x.apps[*last.ToID]; b != nil {
					to = placement.Of(b, x.hostPlace(b.HostID)).Project
				}
			case model.LinkNodeService:
				if s := x.services[*last.ToID]; s != nil {
					to = x.hostPlace(s.HostID).Project
				}
			case model.LinkNodeWebserver:
				if w := x.webservers[*last.ToID]; w != nil {
					to = x.hostPlace(w.HostID).Project
				}
			case model.LinkNodeHost:
				to = x.hostPlace(*last.ToID).Project
			}
			if d := x.projects[to]; d != nil && to != projectID && !slices.Contains(deps, d) {
				deps = append(deps, d)
			}
		}
	}
	slices.SortFunc(deps, func(a, b *model.Project) int { return cmp.Compare(a.Slug, b.Slug) })
	return deps, externals
}
