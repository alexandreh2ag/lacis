// Package placement tells the projects and envs a host, a service or a
// webserver belongs to. A host hosts the project and env of its labels, and
// also those of every app it runs: an app may name another project or env
// than its host, it still runs there. A service or a webserver belongs to
// its host's labels and to the apps of its host it serves or that use it.
package placement

import (
	"slices"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
)

// Place is a project and an env; uuid.Nil when unknown.
type Place struct {
	Project uuid.UUID
	Env     uuid.UUID
}

// Places are the places of the hosts, services and webservers of an
// inventory, by id.
type Places struct {
	of map[uuid.UUID][]Place
}

// New computes the places of an inventory.
func New(inv *model.Inventory) *Places {
	p := &Places{of: map[uuid.UUID][]Place{}}
	hosts := make(map[uuid.UUID]Place, len(inv.Hosts))
	for _, h := range inv.Hosts {
		hosts[h.ID] = Place{Project: id(h.ProjectID), Env: id(h.EnvironmentID)}
		p.add(h.ID, hosts[h.ID])
	}
	// hostOf is the host of every app, service and webserver.
	hostOf := map[uuid.UUID]uuid.UUID{}
	apps := make(map[uuid.UUID]Place, len(inv.Apps))
	for _, a := range inv.Apps {
		hostOf[a.ID] = a.HostID
		apps[a.ID] = Of(&a, hosts[a.HostID])
		p.add(a.HostID, apps[a.ID])
	}
	for _, s := range inv.Services {
		hostOf[s.ID] = s.HostID
		p.add(s.ID, hosts[s.HostID])
	}
	for _, w := range inv.Webservers {
		hostOf[w.ID] = w.HostID
		p.add(w.ID, hosts[w.HostID])
	}
	local := func(l *model.Link) bool {
		if l.ToID == nil {
			return false
		}
		from, ok := hostOf[l.FromID]
		to, found := hostOf[*l.ToID]
		return ok && found && from == to
	}
	// A component gets the places of the apps of its host it serves or
	// that use it.
	for i := range inv.Links {
		l := &inv.Links[i]
		if !local(l) {
			continue
		}
		if app, ok := apps[l.FromID]; ok && l.ToType != model.LinkNodeApp {
			p.add(*l.ToID, app)
		}
		if app, ok := apps[*l.ToID]; ok && l.FromType == model.LinkNodeWebserver {
			p.add(l.FromID, app)
		}
	}
	// A service reached by a local webserver serves its apps too (php-fpm
	// behind nginx).
	for i := range inv.Links {
		l := &inv.Links[i]
		if local(l) && l.FromType == model.LinkNodeWebserver && l.ToType == model.LinkNodeService {
			for _, place := range p.of[l.FromID] {
				p.add(*l.ToID, place)
			}
		}
	}
	return p
}

// Of is the place of an app: its own project and env, else those of its
// host.
func Of(a *model.App, host Place) Place {
	place := Place{Project: id(a.ProjectID), Env: id(a.EnvironmentID)}
	if place.Project == uuid.Nil {
		place.Project = host.Project
	}
	if place.Env == uuid.Nil {
		place.Env = host.Env
	}
	return place
}

// Of returns the places of a host, a service or a webserver, those of the
// labels of its host first.
func (p *Places) Of(id uuid.UUID) []Place { return p.of[id] }

// Any tells whether one of the places of id keeps.
func (p *Places) Any(id uuid.UUID, keep func(Place) bool) bool {
	return slices.ContainsFunc(p.of[id], keep)
}

func (p *Places) add(id uuid.UUID, place Place) {
	if place != (Place{}) && !slices.Contains(p.of[id], place) {
		p.of[id] = append(p.of[id], place)
	}
}

func id(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}
