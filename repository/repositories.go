// Package repository holds the data access, one interface per entity. A
// service opens a transaction with db.Transaction and passes its *gorm.DB
// to the repositories through WithTx.
package repository

import "gorm.io/gorm"

type Repositories struct {
	// DB is the connection, or the transaction, the repositories are bound
	// to; the job queue inserts its jobs in it.
	DB              *gorm.DB
	Namespace       NamespaceRepository
	Network         NetworkRepository
	EnrollmentToken EnrollmentTokenRepository
	Agent           AgentRepository
	Host            HostRepository
	Snapshot        SnapshotRepository
	User            UserRepository
	Role            RoleRepository
	Project         ProjectRepository
	Environment     EnvironmentRepository
	HostIP          HostIPRepository
	Runtime         RuntimeRepository
	Service         ServiceRepository
	Endpoint        EndpointRepository
	Webserver       WebserverRepository
	App             AppRepository
	AppDependency   AppDependencyRepository
	Inventory       InventoryRepository
	Link            LinkRepository
	ChangeEvent     ChangeEventRepository
	Session         SessionRepository
	TechIcon        TechIconRepository
}

func NewRepositories(db *gorm.DB) *Repositories {
	return &Repositories{
		DB:              db,
		Namespace:       NewNamespaceRepository(db),
		Network:         NewNetworkRepository(db),
		EnrollmentToken: NewEnrollmentTokenRepository(db),
		Agent:           NewAgentRepository(db),
		Host:            NewHostRepository(db),
		Snapshot:        NewSnapshotRepository(db),
		User:            NewUserRepository(db),
		Role:            NewRoleRepository(db),
		Project:         NewProjectRepository(db),
		Environment:     NewEnvironmentRepository(db),
		HostIP:          NewHostIPRepository(db),
		Runtime:         NewRuntimeRepository(db),
		Service:         NewServiceRepository(db),
		Endpoint:        NewEndpointRepository(db),
		Webserver:       NewWebserverRepository(db),
		App:             NewAppRepository(db),
		AppDependency:   NewAppDependencyRepository(db),
		Inventory:       NewInventoryRepository(db),
		Link:            NewLinkRepository(db),
		ChangeEvent:     NewChangeEventRepository(db),
		Session:         NewSessionRepository(db),
		TechIcon:        NewTechIconRepository(db),
	}
}
