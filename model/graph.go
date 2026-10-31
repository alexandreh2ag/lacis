package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	LinkSourceEnv       = "env"
	LinkSourceTraefik   = "traefik"
	LinkSourceWebserver = "webserver"
	LinkSourceLauncher  = "launcher"
	LinkSourceProcess   = "process"

	LinkConfidenceExact     = "exact"
	LinkConfidenceInferred  = "inferred"
	LinkConfidenceAmbiguous = "ambiguous"

	// The ends of a link: an app, a service or a webserver, a host when the
	// port targeted has no owner, or an external target, which has no id.
	LinkNodeApp       = "app"
	LinkNodeService   = "service"
	LinkNodeWebserver = "webserver"
	LinkNodeHost      = "host"
	LinkNodeExternal  = "external"
)

type Link struct {
	Base
	NamespaceID    uuid.UUID `gorm:"type:uuid"`
	FromType       string
	FromID         uuid.UUID `gorm:"type:uuid"`
	ToType         string
	ToID           *uuid.UUID `gorm:"type:uuid"`
	ExternalTarget *string
	Source         string
	Confidence     string
	Detail         json.RawMessage `gorm:"type:jsonb;serializer:json;not null"`
	ComputedAt     time.Time
}

func (Link) TableName() string { return "links" }

const (
	ChangeAdded   = "added"
	ChangeRemoved = "removed"
	ChangeChanged = "changed"

	// The entities a change event is about. A link has no stable id, its
	// rows being replaced at each computation: its event carries the id of
	// the app or webserver it starts from.
	ChangeEntityHost      = "host"
	ChangeEntityRuntime   = "runtime"
	ChangeEntityService   = "service"
	ChangeEntityWebserver = "webserver"
	ChangeEntityApp       = "app"
	ChangeEntityLink      = "link"
)

// ChangeEvent is a change of the inventory: an entity added, removed, or
// some of its fields changed. Before and after hold the fields naming the
// entity and the fields that changed.
type ChangeEvent struct {
	ID          int64      `gorm:"primaryKey;autoIncrement"`
	NamespaceID uuid.UUID  `gorm:"type:uuid"`
	HostID      *uuid.UUID `gorm:"type:uuid"`
	EntityType  string
	EntityID    *uuid.UUID `gorm:"type:uuid"`
	Kind        string
	Before      json.RawMessage `gorm:"type:jsonb;serializer:json"`
	After       json.RawMessage `gorm:"type:jsonb;serializer:json"`
	OccurredAt  time.Time
}

func (ChangeEvent) TableName() string { return "change_events" }
