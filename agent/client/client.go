// Package client calls the agent API of lacis-server.
package client

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/common/api"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/common/version"
	"github.com/spf13/afero"
)

// Timeout bounds every request, body included.
const Timeout = 30 * time.Second

// maxErrorBody bounds what is read of an error response.
const maxErrorBody = 64 << 10

type Client interface {
	// Enroll exchanges an enrollment token for the identity of the agent.
	Enroll(ctx context.Context, enrollmentToken string) (*api.EnrollResponse, error)
	// SendSnapshot sends s compressed with gzip, authenticated with the
	// agent token, and returns what the server did with it.
	SendSnapshot(ctx context.Context, agentToken string, s *snapshot.Snapshot) (api.SnapshotStatus, error)
}

// Error is an error answered by the server.
type Error struct {
	Status int
	Code   api.ErrorCode
	// Message is the message of the server, or the start of its body when
	// it is not an api.Error.
	Message string
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("server answered %d", e.Status)
	if e.Code != "" {
		msg += " " + string(e.Code)
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

type client struct {
	baseURL string
	http    *http.Client
}

// New returns a client of the server described by cfg; fs reads its CA file.
func New(cfg config.ServerConfig, fs afero.Fs) (Client, error) {
	tlsConfig, err := cfg.TLSConfig(fs)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	return &client{
		baseURL: strings.TrimSuffix(cfg.URL, "/") + "/",
		http:    &http.Client{Transport: transport, Timeout: Timeout},
	}, nil
}

func (c *client) Enroll(ctx context.Context, enrollmentToken string) (*api.EnrollResponse, error) {
	req, err := c.newRequest(ctx, api.PathEnroll, enrollmentToken, nil)
	if err != nil {
		return nil, err
	}
	resp := &api.EnrollResponse{}
	if err := c.do(req, api.PathEnroll, resp, http.StatusCreated); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *client) SendSnapshot(ctx context.Context, agentToken string, s *snapshot.Snapshot) (api.SnapshotStatus, error) {
	body := &bytes.Buffer{}
	gz := gzip.NewWriter(body)
	if err := json.NewEncoder(gz).Encode(s); err != nil {
		return "", fmt.Errorf("encode snapshot: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("compress snapshot: %w", err)
	}
	req, err := c.newRequest(ctx, api.PathSnapshot, agentToken, body)
	if err != nil {
		return "", err
	}
	req.Header.Set(api.HeaderContentType, api.ContentTypeJSON)
	req.Header.Set(api.HeaderContentEncoding, api.EncodingGzip)
	req.Header.Set(api.HeaderSchemaVersion, strconv.Itoa(s.SchemaVersion))
	resp := &api.SnapshotResponse{}
	if err := c.do(req, api.PathSnapshot, resp, http.StatusAccepted, http.StatusOK); err != nil {
		return "", err
	}
	return resp.Status, nil
}

// newRequest returns a POST on path, authenticated with token.
func (c *client) newRequest(ctx context.Context, path, token string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set(api.HeaderAuthorization, api.BearerPrefix+token)
	req.Header.Set(api.HeaderAgentVersion, version.Version)
	req.Header.Set("User-Agent", "lacis-agent/"+version.Version)
	return req, nil
}

// do sends req and decodes the answer into out when its status is one of
// want, or returns an *Error.
func (c *client) do(req *http.Request, path string, out any, want ...int) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if !slices.Contains(want, resp.StatusCode) {
		return decodeError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s answer: %w", path, err)
	}
	return nil
}

func decodeError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	e := &Error{Status: resp.StatusCode}
	apiErr := api.Error{}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Code != "" {
		e.Code, e.Message = apiErr.Code, apiErr.Message
		return e
	}
	e.Message = strings.TrimSpace(string(body))
	if len(e.Message) > 200 {
		e.Message = e.Message[:200] + "…"
	}
	return e
}
