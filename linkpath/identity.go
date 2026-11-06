package linkpath

import (
	"strings"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
)

// Sep joins the parts of an identity.
const Sep = "\x1f"

// Identities name the ends of the links whatever their hosts: an app is its
// name in its project and env, the unicity that holds, its deployments are
// one.
type Identities struct {
	hosts      map[uuid.UUID]*model.Host
	apps       map[uuid.UUID]*model.App
	services   map[uuid.UUID]*model.Service
	webservers map[uuid.UUID]*model.Webserver
}

// NewIdentities indexes the ends of an inventory.
func NewIdentities(inv *model.Inventory) *Identities {
	x := &Identities{hosts: map[uuid.UUID]*model.Host{}, apps: map[uuid.UUID]*model.App{},
		services: map[uuid.UUID]*model.Service{}, webservers: map[uuid.UUID]*model.Webserver{}}
	for i := range inv.Hosts {
		x.hosts[inv.Hosts[i].ID] = &inv.Hosts[i]
	}
	for i := range inv.Apps {
		x.apps[inv.Apps[i].ID] = &inv.Apps[i]
	}
	for i := range inv.Services {
		x.services[inv.Services[i].ID] = &inv.Services[i]
	}
	for i := range inv.Webservers {
		x.webservers[inv.Webservers[i].ID] = &inv.Webservers[i]
	}
	return x
}

// Of names an end of a link: an app, a service or a webserver by its
// project, env and name; a host, or an end of a host without project or
// env, by its id; an external target by itself.
func (x *Identities) Of(kind string, id *uuid.UUID, external *string) string {
	if id == nil {
		ext := ""
		if external != nil {
			ext = *external
		}
		return strings.Join([]string{model.LinkNodeExternal, ext}, Sep)
	}
	place := func(hostID uuid.UUID, projectID, envID *uuid.UUID, name string) string {
		if projectID == nil && envID == nil {
			if h := x.hosts[hostID]; h != nil {
				projectID, envID = h.ProjectID, h.EnvironmentID
			}
		}
		if projectID == nil || envID == nil {
			return strings.Join([]string{kind, id.String()}, Sep)
		}
		return strings.Join([]string{kind, projectID.String(), envID.String(), name}, Sep)
	}
	switch kind {
	case model.LinkNodeApp:
		if a := x.apps[*id]; a != nil {
			return place(a.HostID, a.ProjectID, a.EnvironmentID, a.Name)
		}
	case model.LinkNodeService:
		if s := x.services[*id]; s != nil {
			return place(s.HostID, nil, nil, s.Type+" "+s.Instance)
		}
	case model.LinkNodeWebserver:
		if w := x.webservers[*id]; w != nil {
			return place(w.HostID, nil, nil, w.Type)
		}
	}
	return strings.Join([]string{kind, id.String()}, Sep)
}
