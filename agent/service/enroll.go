package service

import (
	stdContext "context"
	"errors"
	"fmt"

	"github.com/alexandreh2ag/lacis/agent/client"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/state"
)

var ErrNoEnrollmentToken = errors.New("auth.enrollment_token is not set")

// Enroll returns the identity kept in the state directory or, when there is
// none, enrolls the agent with the enrollment token and keeps the identity
// received. enrolled reports whether the agent has just been enrolled.
func Enroll(stdCtx stdContext.Context, ctx *context.Context, c client.Client) (id *state.Identity, enrolled bool, err error) {
	dir := ctx.Config.Auth.StateDir
	id, err = state.Load(ctx.Fs, dir)
	if !errors.Is(err, state.ErrNotEnrolled) {
		return id, false, err
	}
	token := ctx.Config.Auth.EnrollmentToken
	if token == "" {
		return nil, false, ErrNoEnrollmentToken
	}
	resp, err := c.Enroll(stdCtx, token)
	if err != nil {
		return nil, false, fmt.Errorf("enroll: %w", err)
	}
	id = &state.Identity{AgentID: resp.AgentID, AgentToken: resp.AgentToken}
	if err := state.Save(ctx.Fs, dir, id); err != nil {
		return nil, false, fmt.Errorf("agent %s enrolled but its identity was not kept, enroll it again: %w", id.AgentID, err)
	}
	return id, true, nil
}
