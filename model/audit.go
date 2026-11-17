package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AuditSource is where a user did what an audit event records.
type AuditSource string

const (
	// AuditMCP is a call to a tool of the MCP server.
	AuditMCP AuditSource = "mcp"
	// AuditUI is an action in the front: a GraphQL mutation, a logo sent.
	AuditUI AuditSource = "ui"
	// AuditAuth is a sign-in, or an MCP client authorized.
	AuditAuth AuditSource = "auth"
)

// AuditEvent records what a user did. Namespace, Project and Target name
// what it was done to, when known; Details holds the rest, as the
// arguments of the call.
type AuditEvent struct {
	ID        int64 `gorm:"primaryKey;autoIncrement"`
	At        time.Time
	UserID    *uuid.UUID `gorm:"type:uuid"`
	Username  string
	Source    AuditSource
	Action    string
	Namespace *string
	Project   *string
	Target    *string
	Details   json.RawMessage `gorm:"type:jsonb;serializer:json"`
}

func (AuditEvent) TableName() string { return "audit_events" }
