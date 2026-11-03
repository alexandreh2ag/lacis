// Package model holds the GORM structs mapping the tables, without logic.
// The schema itself is defined by the SQL migrations.
package model

import (
	"time"

	"github.com/google/uuid"
)

type Namespace struct {
	Base
	Slug              string
	Name              string
	Description       *string
	StaleAfter        Interval `gorm:"type:interval;default:(interval '7 days')"`
	InactiveAfter     Interval `gorm:"type:interval;default:(interval '30 days')"`
	SnapshotRetention Interval `gorm:"type:interval;default:(interval '30 days')"`
	CreatedAt         time.Time
}

func (Namespace) TableName() string { return "namespaces" }

const (
	NetworkKindPrivate = "private"
	NetworkKindPublic  = "public"
)

type Network struct {
	Base
	NamespaceID uuid.UUID `gorm:"type:uuid"`
	Slug        string
	Name        string
	Kind        string `gorm:"default:private"`
	Description *string
	// SplitFromID is the network this one was split from: it shares its
	// range, and holds the hosts moved into it by hand.
	SplitFromID *uuid.UUID `gorm:"type:uuid"`
}

func (Network) TableName() string { return "networks" }

// HostNetwork moves a host out of the network its IPs give, FromNetworkID,
// into NetworkID, a network split from it.
type HostNetwork struct {
	HostID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	FromNetworkID uuid.UUID `gorm:"type:uuid;primaryKey"`
	NetworkID     uuid.UUID `gorm:"type:uuid"`
}

func (HostNetwork) TableName() string { return "host_networks" }

type NetworkCIDR struct {
	NetworkID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	NamespaceID uuid.UUID `gorm:"type:uuid"`
	CIDR        CIDR      `gorm:"column:cidr;type:cidr;primaryKey"`
}

func (NetworkCIDR) TableName() string { return "network_cidrs" }

type Project struct {
	Base
	NamespaceID uuid.UUID `gorm:"type:uuid"`
	Slug        string
	Name        string
	Description *string
}

func (Project) TableName() string { return "projects" }

type Environment struct {
	Base
	NamespaceID uuid.UUID `gorm:"type:uuid"`
	Slug        string
	Name        string
	SortOrder   int
}

func (Environment) TableName() string { return "environments" }

// TechIcon is the logo uploaded for a technology, shown in place of the one
// the front embeds.
type TechIcon struct {
	Tech        string `gorm:"primaryKey"`
	ContentType string
	Content     []byte
	UpdatedBy   *uuid.UUID `gorm:"type:uuid"`
	UpdatedAt   time.Time
}

func (TechIcon) TableName() string { return "tech_icons" }
