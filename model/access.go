package model

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type User struct {
	Base
	Username     string
	FirstName    *string
	LastName     *string
	PasswordHash *string
	OIDCSubject  *string `gorm:"column:oidc_subject"`
	CreatedAt    time.Time
}

func (User) TableName() string { return "users" }

// FullName is the first and last names, empty when the user has none.
func (u User) FullName() string {
	return strings.TrimSpace(strings.Join([]string{deref(u.FirstName), deref(u.LastName)}, " "))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// RoleAdmin is the code of the role holding every permission, created by
// the migrations.
const RoleAdmin = "admin"

// Role is a named set of permissions, given to users and OIDC groups.
type Role struct {
	Base
	Code        string
	Description *string
	CreatedAt   time.Time
}

func (Role) TableName() string { return "roles" }

type UserRole struct {
	UserID uuid.UUID `gorm:"type:uuid;primaryKey"`
	RoleID uuid.UUID `gorm:"type:uuid;primaryKey"`
}

func (UserRole) TableName() string { return "user_roles" }

// RoleOIDCGroup gives the role to the members of an OIDC group.
type RoleOIDCGroup struct {
	RoleID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	GroupName string    `gorm:"primaryKey"`
}

func (RoleOIDCGroup) TableName() string { return "role_oidc_groups" }

type (
	Resource string
	Action   string
)

const (
	// Resources of a namespace.
	ResourceInventory Resource = "inventory" // hosts, apps, graph, manual host deletion
	ResourceMetadata  Resource = "metadata"  // name, description, env order, thresholds

	// Resources belonging to no namespace.
	ResourceNamespaces Resource = "namespaces"
	ResourceTokens     Resource = "tokens"
	ResourceUsers      Resource = "users"
	ResourceRoles      Resource = "roles"
	ResourceIcons      Resource = "icons" // the logos of the technologies
	ResourceTasks      Resource = "tasks" // the scheduled tasks and their runs

	ResourceAll Resource = "*"

	ActionRead  Action = "read"
	ActionWrite Action = "write"
	ActionAll   Action = "*"
)

// NamespacePermission grants an action on a resource of a namespace, or of
// every namespace when NamespaceID is nil.
type NamespacePermission struct {
	Base
	RoleID      uuid.UUID  `gorm:"type:uuid"`
	NamespaceID *uuid.UUID `gorm:"type:uuid"`
	Resource    Resource
	Action      Action
}

func (NamespacePermission) TableName() string { return "namespace_permissions" }

// Allows reports whether the permission grants action on resource in the
// namespace namespaceID.
func (p NamespacePermission) Allows(namespaceID uuid.UUID, resource Resource, action Action) bool {
	return (p.NamespaceID == nil || *p.NamespaceID == namespaceID) && matches(p.Resource, p.Action, resource, action)
}

// GlobalPermission grants an action on a resource belonging to no
// namespace.
type GlobalPermission struct {
	Base
	RoleID   uuid.UUID `gorm:"type:uuid"`
	Resource Resource
	Action   Action
}

func (GlobalPermission) TableName() string { return "global_permissions" }

// Allows reports whether the permission grants action on resource.
func (p GlobalPermission) Allows(resource Resource, action Action) bool {
	return matches(p.Resource, p.Action, resource, action)
}

func matches(grantedResource Resource, grantedAction Action, resource Resource, action Action) bool {
	return (grantedResource == ResourceAll || grantedResource == resource) &&
		(grantedAction == ActionAll || grantedAction == action)
}

// Session is a session of the front. ID is the hash of the token the
// cookie holds.
type Session struct {
	ID         string      `gorm:"primaryKey"`
	UserID     uuid.UUID   `gorm:"type:uuid"`
	OIDCGroups StringArray `gorm:"column:oidc_groups;type:text[];not null"`
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

func (Session) TableName() string { return "sessions" }
