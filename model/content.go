package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Runtime struct {
	Base
	NamespaceID uuid.UUID `gorm:"type:uuid"`
	HostID      uuid.UUID `gorm:"type:uuid"`
	Type        string
	Version     string
	Path        string
	Manager     *string
	// PackageVersion is the version of the system package, revision
	// included; nil without package.
	PackageVersion *string
	FirstSeenAt    time.Time
	RemovedAt      *time.Time
}

func (Runtime) TableName() string { return "runtimes" }

// Container is a Docker container of a host, running or not.
type Container struct {
	Base
	NamespaceID uuid.UUID `gorm:"type:uuid"`
	HostID      uuid.UUID `gorm:"type:uuid"`
	Name        string
	// ContainerID is the short id Docker gives the container: it changes
	// when the container is created again under the same name.
	ContainerID    string
	Image          string
	Version        *string
	State          string
	Ports          []string `gorm:"type:jsonb;serializer:json;not null"`
	Networks       []string `gorm:"type:jsonb;serializer:json;not null"`
	ComposeProject *string
	ComposeService *string
	FirstSeenAt    time.Time
	RemovedAt      *time.Time
}

func (Container) TableName() string { return "containers" }

// BeforeSave stores a nil list as an empty JSON array: the serializer
// would write nothing, which is no JSON.
func (c *Container) BeforeSave(*gorm.DB) error {
	if c.Ports == nil {
		c.Ports = []string{}
	}
	if c.Networks == nil {
		c.Networks = []string{}
	}
	return nil
}

type Service struct {
	Base
	NamespaceID uuid.UUID `gorm:"type:uuid"`
	HostID      uuid.UUID `gorm:"type:uuid"`
	Type        string
	Instance    string `gorm:"default:default"`
	Version     *string
	// PackageVersion is the version of the system package, revision
	// included; nil without package.
	PackageVersion *string
	Details        json.RawMessage `gorm:"type:jsonb;serializer:json;not null"`
	FirstSeenAt    time.Time
	RemovedAt      *time.Time
}

func (Service) TableName() string { return "services" }

type App struct {
	Base
	NamespaceID      uuid.UUID `gorm:"type:uuid"`
	HostID           uuid.UUID `gorm:"type:uuid"`
	Path             string
	ResolvedPath     *string
	ProjectID        *uuid.UUID `gorm:"type:uuid"`
	EnvironmentID    *uuid.UUID `gorm:"type:uuid"`
	Name             string
	Role             string
	Framework        *string
	FrameworkVersion *string
	Runtime          json.RawMessage `gorm:"type:jsonb;serializer:json"`
	Code             json.RawMessage `gorm:"type:jsonb;serializer:json"`
	Launchers        json.RawMessage `gorm:"type:jsonb;serializer:json;not null"`
	Vhosts           StringArray     `gorm:"type:text[];not null"`
	Info             json.RawMessage `gorm:"type:jsonb;serializer:json"`
	FirstSeenAt      time.Time
	RemovedAt        *time.Time
}

func (App) TableName() string { return "apps" }

// Webserver is a webserver of a host with its routing, as the snapshot
// gives it.
type Webserver struct {
	Base
	NamespaceID uuid.UUID `gorm:"type:uuid"`
	HostID      uuid.UUID `gorm:"type:uuid"`
	Type        string
	Version     *string
	Routing     json.RawMessage `gorm:"type:jsonb;serializer:json;not null"`
	FirstSeenAt time.Time
	RemovedAt   *time.Time
}

func (Webserver) TableName() string { return "webservers" }

const (
	EndpointKindTCP    = "tcp"
	EndpointKindUnix   = "unix"
	EndpointKindDomain = "domain"

	EndpointOwnerService   = "service"
	EndpointOwnerApp       = "app"
	EndpointOwnerWebserver = "webserver"
)

type Endpoint struct {
	Base
	NamespaceID uuid.UUID `gorm:"type:uuid"`
	HostID      uuid.UUID `gorm:"type:uuid"`
	Kind        string
	Address     string
	Port        *int
	OwnerType   string
	OwnerID     uuid.UUID `gorm:"type:uuid"`
	RemovedAt   *time.Time
}

func (Endpoint) TableName() string { return "endpoints" }

type AppDependency struct {
	Base
	AppID    uuid.UUID `gorm:"type:uuid"`
	Key      string
	Scheme   *string
	Port     *int
	Path     *string
	Target   json.RawMessage `gorm:"type:jsonb;serializer:json"`
	Source   json.RawMessage `gorm:"type:jsonb;serializer:json;not null"`
	Unparsed bool
}

func (AppDependency) TableName() string { return "app_dependencies" }
