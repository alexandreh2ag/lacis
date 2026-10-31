package model

import (
	"encoding/json"
	"time"

	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/google/uuid"
)

// EnrollmentToken enrolls agents into any namespace, the one given by their
// namespace label. A nil ExpiresAt never expires.
type EnrollmentToken struct {
	Base
	TokenHash string
	Label     *string
	CreatedAt time.Time `gorm:"default:now()"`
	ExpiresAt *time.Time
	RevokedAt *time.Time
}

func (EnrollmentToken) TableName() string { return "enrollment_tokens" }

// Usable reports whether the token can still enroll an agent at now.
func (t EnrollmentToken) Usable(now time.Time) bool {
	return t.RevokedAt == nil && (t.ExpiresAt == nil || now.Before(*t.ExpiresAt))
}

// Agent is an enrolled agent: an identity and the token it authenticates
// with. Its host is created from its first snapshot.
type Agent struct {
	Base
	TokenHash         string
	EnrollmentTokenID uuid.UUID `gorm:"type:uuid"`
	CreatedAt         time.Time `gorm:"default:now()"`
	LastSeenAt        *time.Time
	RevokedAt         *time.Time
	// ConflictAt is when two machines were found sharing the token of the
	// agent, a cloned host; ConflictHostnames are their hostnames.
	ConflictAt        *time.Time
	ConflictHostnames StringArray `gorm:"type:text[]"`
}

func (Agent) TableName() string { return "agents" }

// Host is a machine, as described by the snapshots of its agent.
type Host struct {
	Base
	AgentID          uuid.UUID `gorm:"type:uuid"`
	NamespaceID      uuid.UUID `gorm:"type:uuid"`
	Hostname         string
	Labels           labels.Labels   `gorm:"type:jsonb;serializer:json;not null"`
	ProjectID        *uuid.UUID      `gorm:"type:uuid"`
	EnvironmentID    *uuid.UUID      `gorm:"type:uuid"`
	OS               json.RawMessage `gorm:"column:os;type:jsonb;serializer:json"`
	Kernel           *string
	Arch             *string
	Virtualization   *string
	Resources        json.RawMessage `gorm:"type:jsonb;serializer:json"`
	AgentVersion     *string
	LastSnapshotHash *string
	FirstSeenAt      time.Time
	LastSeenAt       time.Time
}

func (Host) TableName() string { return "hosts" }

// HostIP is an IP of a host. Its network is the one whose CIDR contains it.
type HostIP struct {
	HostID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	Interface *string
	IP        IP `gorm:"column:ip;type:inet;primaryKey"`
	// PrefixLen is the prefix of the subnet of the interface.
	PrefixLen    int
	DefaultRoute bool
}

func (HostIP) TableName() string { return "host_ips" }

// Snapshot is a snapshot as received from an agent, kept for normalization
// and history.
type Snapshot struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	HostID        uuid.UUID `gorm:"type:uuid"`
	CollectedAt   time.Time
	ReceivedAt    time.Time `gorm:"default:now()"`
	Hash          string
	SchemaVersion int
	Payload       json.RawMessage `gorm:"type:jsonb;serializer:json;not null"`
}

func (Snapshot) TableName() string { return "snapshots" }
