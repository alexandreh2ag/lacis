// Package history turns the changes of the inventory into change events:
// an entity added or removed, the fields of an entity that changed.
package history

import (
	"encoding/json"
	"maps"
	"slices"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
)

// State is the fields of an entity the history follows, by name. A field
// whose change is noise, as the order of domains or a Docker IP, is left
// out.
type State map[string]any

// Recorder collects the change events of one normalization or one
// computation of the links.
type Recorder struct {
	namespaceID uuid.UUID
	at          time.Time
	events      []model.ChangeEvent
}

func NewRecorder(namespaceID uuid.UUID, at time.Time) *Recorder {
	return &Recorder{namespaceID: namespaceID, at: at}
}

// Events returns the events recorded, in the order of their recording.
func (r *Recorder) Events() []model.ChangeEvent {
	return r.events
}

// Added records an entity seen for the first time, or seen again after its
// removal.
func (r *Recorder) Added(entityType string, entityID uuid.UUID, hostID *uuid.UUID, after State) {
	r.add(entityType, entityID, hostID, model.ChangeAdded, nil, canonical(after))
}

// Removed records an entity gone.
func (r *Recorder) Removed(entityType string, entityID uuid.UUID, hostID *uuid.UUID, before State) {
	r.add(entityType, entityID, hostID, model.ChangeRemoved, canonical(before), nil)
}

// Changed records the fields that differ between before and after, with
// the identity fields naming the entity on both sides; nothing when no
// field differs.
func (r *Recorder) Changed(entityType string, entityID uuid.UUID, hostID *uuid.UUID, before, after State, identity ...string) {
	b, a := canonical(before), canonical(after)
	changedB, changedA := map[string]any{}, map[string]any{}
	for _, k := range slices.Sorted(maps.Keys(unionKeys(b, a))) {
		if !equal(b[k], a[k]) {
			changedB[k], changedA[k] = b[k], a[k]
		}
	}
	if len(changedA) == 0 {
		return
	}
	for _, k := range identity {
		changedB[k], changedA[k] = b[k], a[k]
	}
	r.add(entityType, entityID, hostID, model.ChangeChanged, changedB, changedA)
}

func (r *Recorder) add(entityType string, entityID uuid.UUID, hostID *uuid.UUID, kind string, before, after map[string]any) {
	id := entityID
	r.events = append(r.events, model.ChangeEvent{NamespaceID: r.namespaceID, HostID: hostID, EntityType: entityType,
		EntityID: &id, Kind: kind, Before: marshal(before), After: marshal(after), OccurredAt: r.at})
}

// canonical decodes the state as JSON gives it back: a nil pointer is nil,
// a struct and a JSON text are maps, so that a state built from a snapshot
// and one built from the database compare equal.
func canonical(s State) map[string]any {
	if s == nil {
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func unionKeys(a, b map[string]any) map[string]bool {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return keys
}

func equal(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

func marshal(m map[string]any) json.RawMessage {
	if m == nil {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}
