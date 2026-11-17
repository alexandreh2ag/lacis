package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
)

// AuditAbout names what an action was done to; an empty field is unknown.
type AuditAbout struct {
	Namespace string
	Project   string
	Target    string
}

// AuditService records what the users did, and reads it back.
type AuditService interface {
	// Record records that user did action; details are its arguments,
	// passwords and secrets left out. Recording never fails the action,
	// already done: an error is logged.
	Record(ctx context.Context, user model.User, source model.AuditSource, action string, about AuditAbout, details any)
	// List returns a page of the events of the filter, newest first, and
	// how many match.
	List(ctx context.Context, filter repository.AuditFilter) ([]model.AuditEvent, int64, error)
	// Usernames returns the usernames of the events containing search.
	Usernames(ctx context.Context, search string) ([]string, error)
	// Actions returns the actions of the events containing search.
	Actions(ctx context.Context, search string) ([]string, error)
}

// The actions of the sign-ins.
const (
	AuditLogin = "login"
	// AuditMCPAuthorize is an MCP client authorized by the user.
	AuditMCPAuthorize = "mcp_authorize"
)

// MaxAuditUsernames bounds the usernames Usernames returns, and the
// actions Actions returns.
const MaxAuditUsernames = 20

type auditService struct {
	appCtx *appContext.Context
	events repository.AuditRepository
}

func NewAuditService(appCtx *appContext.Context, events repository.AuditRepository) AuditService {
	return &auditService{appCtx: appCtx, events: events}
}

func (s *auditService) Record(ctx context.Context, user model.User, source model.AuditSource, action string, about AuditAbout, details any) {
	event := newAuditEvent(s.appCtx.Clock.Now(), user, source, action, about, details)
	// The request may be over: the event is recorded all the same.
	if err := s.events.Create(context.WithoutCancel(ctx), event); err != nil {
		s.appCtx.Logger.Error("audit event not recorded", "action", action, "user", user.Username, "error", err)
	}
}

func (s *auditService) List(ctx context.Context, filter repository.AuditFilter) ([]model.AuditEvent, int64, error) {
	return s.events.List(ctx, filter)
}

func (s *auditService) Usernames(ctx context.Context, search string) ([]string, error) {
	return s.events.Usernames(ctx, search, MaxAuditUsernames)
}

func (s *auditService) Actions(ctx context.Context, search string) ([]string, error) {
	return s.events.Actions(ctx, search, MaxAuditUsernames)
}

// newAuditEvent is the event of user doing action at at.
func newAuditEvent(at time.Time, user model.User, source model.AuditSource, action string, about AuditAbout, details any) *model.AuditEvent {
	event := &model.AuditEvent{At: at, Username: user.Username, Source: source, Action: action,
		Namespace: optional(strings.TrimSpace(about.Namespace)), Project: optional(strings.TrimSpace(about.Project)),
		Target:  optional(strings.TrimSpace(about.Target)),
		Details: auditDetails(details)}
	if user.ID != uuid.Nil {
		id := user.ID
		event.UserID = &id
	}
	return event
}

// auditDetails is v as a JSON object, the values of the keys naming a
// password or a secret left out; {} when v is nil or no object.
func auditDetails(v any) json.RawMessage {
	empty := json.RawMessage("{}")
	if v == nil {
		return empty
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return empty
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return empty
	}
	out, err := json.Marshal(redact(obj))
	if err != nil {
		return empty
	}
	return out
}

// redact replaces, at any depth, the values of the keys naming a password
// or a secret.
func redact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			key := strings.ToLower(k)
			if strings.Contains(key, "password") || strings.Contains(key, "secret") {
				x[k] = "***"
				continue
			}
			x[k] = redact(val)
		}
	case []any:
		for i := range x {
			x[i] = redact(x[i])
		}
	}
	return v
}
