// Package agent exposes the agent API, described in common/api.
package agent

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/alexandreh2ag/lacis/common/api"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/labstack/echo/v5"
)

type Handler struct {
	logger    *slog.Logger
	cfg       config.AgentAPIConfig
	agents    service.AgentService
	snapshots service.SnapshotService
}

func NewHandler(logger *slog.Logger, cfg config.AgentAPIConfig, agents service.AgentService, snapshots service.SnapshotService) *Handler {
	return &Handler{logger: logger, cfg: cfg, agents: agents, snapshots: snapshots}
}

// Register adds the agent routes to the group mounted on the base path.
func (h *Handler) Register(g *echo.Group) {
	g.POST("/"+api.PathEnroll, h.Enroll)
	g.POST("/"+api.PathSnapshot, h.Snapshot)
}

// Enroll creates an agent and returns its identity, once per enrollment.
func (h *Handler) Enroll(c *echo.Context) error {
	secret, ok := bearer(c.Request())
	if !ok {
		return writeError(c, http.StatusUnauthorized, api.ErrCodeInvalidToken, "missing bearer token")
	}
	enrolled, err := h.agents.Enroll(c.Request().Context(), secret)
	if errors.Is(err, service.ErrUnauthorized) {
		return writeError(c, http.StatusUnauthorized, api.ErrCodeInvalidToken, "unknown, expired or revoked enrollment token")
	}
	if err != nil {
		h.logger.Error("enroll agent", "error", err)
		return writeError(c, http.StatusInternalServerError, api.ErrCodeInternal, "")
	}
	return c.JSON(http.StatusCreated, api.EnrollResponse{
		AgentID:    enrolled.Agent.ID.String(),
		AgentToken: enrolled.Secret,
	})
}

// Snapshot receives a snapshot: 202 when stored, 200 when identical to the
// previous one.
func (h *Handler) Snapshot(c *echo.Context) error {
	r := c.Request()
	secret, ok := bearer(r)
	if !ok {
		return writeError(c, http.StatusUnauthorized, api.ErrCodeInvalidToken, "missing bearer token")
	}
	agent, err := h.snapshots.Authenticate(r.Context(), secret)
	if errors.Is(err, service.ErrUnauthorized) {
		return writeError(c, http.StatusUnauthorized, api.ErrCodeInvalidToken, "unknown or revoked agent token")
	}
	if err != nil {
		h.logger.Error("authenticate agent", "error", err)
		return writeError(c, http.StatusInternalServerError, api.ErrCodeInternal, "")
	}

	if r.Header.Get(api.HeaderContentEncoding) != api.EncodingGzip {
		return writeError(c, http.StatusUnsupportedMediaType, api.ErrCodeInvalidEncoding, "the body must be compressed with gzip")
	}
	raw, err := h.readBody(c)
	var tooLarge *tooLargeError
	switch {
	case errors.As(err, &tooLarge):
		return writeError(c, http.StatusRequestEntityTooLarge, api.ErrCodePayloadTooLarge, err.Error())
	case err != nil:
		return writeError(c, http.StatusBadRequest, api.ErrCodeInvalidEncoding, err.Error())
	}
	snap := &snapshot.Snapshot{}
	if err := json.Unmarshal(raw, snap); err != nil {
		return writeError(c, http.StatusUnprocessableEntity, api.ErrCodeInvalidSchema, err.Error())
	}

	result, err := h.snapshots.Ingest(r.Context(), agent, snap, raw)
	switch {
	case errors.Is(err, service.ErrInvalid):
		return writeError(c, http.StatusUnprocessableEntity, api.ErrCodeInvalidSchema, err.Error())
	case errors.Is(err, service.ErrNamespaceMismatch):
		return writeError(c, http.StatusForbidden, api.ErrCodeNamespaceMismatch, err.Error())
	case err != nil:
		h.logger.Error("ingest snapshot", "agent_id", agent.ID, "error", err)
		return writeError(c, http.StatusInternalServerError, api.ErrCodeInternal, "")
	case result == service.Unchanged:
		return c.JSON(http.StatusOK, api.SnapshotResponse{Status: api.SnapshotUnchanged})
	default:
		return c.JSON(http.StatusAccepted, api.SnapshotResponse{Status: api.SnapshotAccepted})
	}
}

// tooLargeError is a body over one of the limits of agent_api.
type tooLargeError struct {
	what  string
	limit config.ByteSize
}

func (e *tooLargeError) Error() string {
	return fmt.Sprintf("%s body exceeds %d bytes", e.what, e.limit)
}

// readBody returns the decompressed body, within the limits of agent_api.
func (h *Handler) readBody(c *echo.Context) ([]byte, error) {
	compressedLimit := h.cfg.MaxBodyCompressed
	tooLargeCompressed := &tooLargeError{what: "compressed", limit: compressedLimit}
	if c.Request().ContentLength > int64(compressedLimit) {
		return nil, tooLargeCompressed
	}
	body := http.MaxBytesReader(c.Response(), c.Request().Body, int64(compressedLimit))
	gz, err := gzip.NewReader(body)
	if err != nil {
		return nil, gzipError(err, tooLargeCompressed)
	}
	limit := int64(h.cfg.MaxBodyDecompressed)
	raw, err := io.ReadAll(io.LimitReader(gz, limit+1))
	if err != nil {
		return nil, gzipError(err, tooLargeCompressed)
	}
	if int64(len(raw)) > limit {
		return nil, &tooLargeError{what: "decompressed", limit: h.cfg.MaxBodyDecompressed}
	}
	return raw, nil
}

func gzipError(err error, tooLarge *tooLargeError) error {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return tooLarge
	}
	return fmt.Errorf("invalid gzip body: %w", err)
}

// bearer returns the token of the Authorization header.
func bearer(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get(api.HeaderAuthorization), api.BearerPrefix)
	token = strings.TrimSpace(token)
	return token, ok && token != ""
}

func writeError(c *echo.Context, status int, code api.ErrorCode, message string) error {
	return c.JSON(status, api.Error{Code: code, Message: message})
}
