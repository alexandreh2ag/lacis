package history

import (
	"encoding/json"
	"time"

	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
)

// linkIdentity are the fields naming a link: its ends, its source and the
// dependency it comes from.
var linkIdentity = []string{"from", "to", "to_type", "source", "key"}

// Links records the links added and removed between the links of the
// inventory, as last computed, and links, computed from it. The first
// computation of a namespace records nothing, nor do the links of an app
// or a webserver added or removed since: the event of the entity tells it.
func Links(r *Recorder, inv *model.Inventory, links []model.Link) {
	if len(inv.Links) == 0 {
		return
	}
	var last time.Time
	for _, l := range inv.Links {
		if l.ComputedAt.After(last) {
			last = l.ComputedAt
		}
	}
	ends := newEnds(inv)
	before := map[string]model.Link{}
	for _, l := range inv.Links {
		before[linkKey(l)] = l
	}
	after := map[string]model.Link{}
	for _, l := range links {
		after[linkKey(l)] = l
	}
	for _, l := range inv.Links {
		from, active := ends.from[l.FromID]
		if !active {
			continue
		}
		if _, ok := after[linkKey(l)]; !ok {
			r.Removed(model.ChangeEntityLink, l.FromID, &from.hostID, ends.state(l))
		}
	}
	for _, l := range links {
		from := ends.from[l.FromID]
		old, ok := before[linkKey(l)]
		switch {
		case ok:
			r.Changed(model.ChangeEntityLink, l.FromID, &from.hostID, ends.state(old), ends.state(l), linkIdentity...)
		case !from.firstSeenAt.After(last):
			r.Added(model.ChangeEntityLink, l.FromID, &from.hostID, ends.state(l))
		}
	}
}

func linkKey(l model.Link) string {
	var d linker.Detail
	_ = json.Unmarshal(l.Detail, &d)
	to, external := "", ""
	if l.ToID != nil {
		to = l.ToID.String()
	}
	if l.ExternalTarget != nil {
		external = *l.ExternalTarget
	}
	return l.FromType + " " + l.FromID.String() + " " + l.ToType + " " + to + " " + external + " " + l.Source + " " + d.Key
}

// defaultInstance is the instance of a service running once on its host.
const defaultInstance = "default"

type end struct {
	label       string
	hostID      uuid.UUID
	firstSeenAt time.Time
}

// ends names the apps, services, webservers and hosts of an inventory.
type ends struct {
	from map[uuid.UUID]end // the apps and webservers, which links start from
	to   map[uuid.UUID]string
}

func newEnds(inv *model.Inventory) *ends {
	e := &ends{from: map[uuid.UUID]end{}, to: map[uuid.UUID]string{}}
	hostnames := map[uuid.UUID]string{}
	for _, h := range inv.Hosts {
		hostnames[h.ID] = h.Hostname
		e.to[h.ID] = h.Hostname
	}
	for _, a := range inv.Apps {
		e.from[a.ID] = end{label: hostnames[a.HostID] + "/" + a.Name, hostID: a.HostID, firstSeenAt: a.FirstSeenAt}
		e.to[a.ID] = e.from[a.ID].label
	}
	for _, w := range inv.Webservers {
		e.from[w.ID] = end{label: hostnames[w.HostID] + "/" + w.Type, hostID: w.HostID, firstSeenAt: w.FirstSeenAt}
		e.to[w.ID] = e.from[w.ID].label
	}
	for _, s := range inv.Services {
		label := hostnames[s.HostID] + "/" + s.Type
		if s.Instance != defaultInstance {
			label += "/" + s.Instance
		}
		e.to[s.ID] = label
	}
	return e
}

// state is what the history follows of a link: its ends, its source, the
// target as written, how sure it is and why. A target removed since is
// named by the target as written.
func (e *ends) state(l model.Link) State {
	var d linker.Detail
	_ = json.Unmarshal(l.Detail, &d)
	to := d.Target
	switch {
	case l.ToID != nil && e.to[*l.ToID] != "":
		to = e.to[*l.ToID]
	case l.ExternalTarget != nil:
		to = *l.ExternalTarget
	}
	return State{"from": e.from[l.FromID].label, "to": to, "to_type": l.ToType, "source": l.Source, "key": d.Key,
		"target": d.Target, "confidence": l.Confidence, "rule": d.Rule, "reason": d.Reason}
}
