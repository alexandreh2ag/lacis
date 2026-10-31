package service

import (
	stdContext "context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"time"

	"github.com/alexandreh2ag/lacis/agent/client"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/state"
)

// Bounds of the delay between two attempts after a transient failure.
const (
	MinBackoff = 10 * time.Second
	MaxBackoff = 5 * time.Minute
)

// ErrTokenRefused stops the agent: its token is unknown or revoked, and only
// a new enrollment can fix it.
var ErrTokenRefused = errors.New("the server refused the token")

// jitter returns a random duration in [0, maxJitter]; tests replace it.
var jitter = func(maxJitter time.Duration) time.Duration {
	if maxJitter <= 0 {
		return 0
	}
	return rand.N(maxJitter + 1)
}

// Run enrolls the agent when needed, then collects and sends a snapshot
// every interval plus a random jitter, until stdCtx is done. Transient
// failures are retried with an exponential backoff; a refused token stops
// it with ErrTokenRefused.
func Run(stdCtx stdContext.Context, ctx *context.Context, c client.Client) error {
	id, err := enrollWithRetry(stdCtx, ctx, c)
	if err != nil || id == nil {
		return err
	}
	ctx.Logger.Info("agent started", "agent_id", id.AgentID, "interval", ctx.Config.Interval, "jitter", ctx.Config.Jitter)

	var backoff time.Duration
	for {
		wait, err := sendOnce(stdCtx, ctx, c, id, &backoff)
		if err != nil || stdCtx.Err() != nil {
			return err
		}
		if !sleep(stdCtx, ctx, wait) {
			return nil
		}
	}
}

// enrollWithRetry enrolls the agent, retrying transient failures. It
// returns a nil identity when stdCtx is done first.
func enrollWithRetry(stdCtx stdContext.Context, ctx *context.Context, c client.Client) (*state.Identity, error) {
	var backoff time.Duration
	for {
		id, enrolled, err := Enroll(stdCtx, ctx, c)
		if err == nil {
			if enrolled {
				ctx.Logger.Info("agent enrolled", "agent_id", id.AgentID)
			}
			return id, nil
		}
		if stdCtx.Err() != nil {
			return nil, nil
		}
		if !transient(err) {
			if refused(err) {
				return nil, fmt.Errorf("%w: check auth.enrollment_token: %w", ErrTokenRefused, err)
			}
			return nil, err
		}
		backoff = nextBackoff(backoff, ctx.Config.Interval)
		ctx.Logger.Warn("enrollment failed, retrying", "error", err, "retry_in", backoff)
		if !sleep(stdCtx, ctx, backoff) {
			return nil, nil
		}
	}
}

// sendOnce collects and sends a snapshot, and returns how long to wait
// before the next one.
func sendOnce(stdCtx stdContext.Context, ctx *context.Context, c client.Client, id *state.Identity, backoff *time.Duration) (time.Duration, error) {
	next := ctx.Config.Interval + jitter(ctx.Config.Jitter)
	s, err := Collect(stdCtx, ctx)
	if stdCtx.Err() != nil {
		return 0, nil
	}
	if err != nil {
		ctx.Logger.Error("collection failed", "error", err, "next_in", next)
		return next, nil
	}
	status, err := c.SendSnapshot(stdCtx, id.AgentToken, s)
	switch {
	case err == nil:
		*backoff = 0
		ctx.Logger.Info("snapshot sent", "status", status, "next_in", next)
		return next, nil
	case stdCtx.Err() != nil:
		return 0, nil
	case refused(err):
		return 0, fmt.Errorf("%w: the agent %s is revoked or unknown, remove %s and enroll it again: %w",
			ErrTokenRefused, id.AgentID, state.IdentityFile, err)
	case transient(err):
		*backoff = nextBackoff(*backoff, ctx.Config.Interval)
		ctx.Logger.Warn("snapshot not sent, retrying", "error", err, "retry_in", *backoff)
		return *backoff, nil
	default:
		// Rejected: sending the same snapshot again would not help.
		*backoff = 0
		ctx.Logger.Error("snapshot rejected", "error", err, "next_in", next)
		return next, nil
	}
}

// transient reports whether an error may go away on its own: a network
// error, a server error or a rate limit.
func transient(err error) bool {
	var apiErr *client.Error
	if errors.As(err, &apiErr) {
		return apiErr.Status >= http.StatusInternalServerError || apiErr.Status == http.StatusTooManyRequests
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

func refused(err error) bool {
	var apiErr *client.Error
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized
}

// nextBackoff doubles backoff from MinBackoff, up to MaxBackoff and to
// interval.
func nextBackoff(backoff, interval time.Duration) time.Duration {
	backoff = max(MinBackoff, 2*backoff)
	return min(backoff, MaxBackoff, max(interval, MinBackoff))
}

// sleep waits for d, and reports false when stdCtx is done first.
func sleep(stdCtx stdContext.Context, ctx *context.Context, d time.Duration) bool {
	select {
	case <-stdCtx.Done():
		return false
	case <-ctx.Clock.After(d):
		return stdCtx.Err() == nil
	}
}
