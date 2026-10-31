package agent

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/common/api"
	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/config"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func gzipped(t *testing.T, b []byte) []byte {
	buf := &bytes.Buffer{}
	gz := gzip.NewWriter(buf)
	_, err := gz.Write(b)
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func validSnapshot(t *testing.T) []byte {
	s := snapshot.New(snapshot.AgentInfo{Version: "1.2.0"}, labels.Labels{Namespace: "acme", Project: "site", Env: "prod"}, "acme-site-prod-1")
	s.CollectedAt = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	b, err := json.Marshal(s)
	require.NoError(t, err)
	return b
}

func TestSnapshot(t *testing.T) {
	agent := &model.Agent{Base: model.Base{ID: uuid.New()}}
	body := validSnapshot(t)
	errDB := errors.New("db down")
	cfg := config.DefaultConfig().AgentAPI
	cfg.MaxBodyCompressed = 2 * config.KB
	cfg.MaxBodyDecompressed = 4 * config.KB

	type call struct {
		authErr   error
		ingest    bool
		result    service.IngestResult
		ingestErr error
	}
	tests := []struct {
		name          string
		authorization string
		encoding      string
		body          []byte
		call          *call
		wantStatus    int
		wantBody      string
	}{
		{name: "accepted", call: &call{ingest: true, result: service.Accepted}, wantStatus: http.StatusAccepted, wantBody: `{"status":"accepted"}`},
		{name: "unchanged", call: &call{ingest: true, result: service.Unchanged}, wantStatus: http.StatusOK, wantBody: `{"status":"unchanged"}`},
		{name: "no token", authorization: "-", wantStatus: http.StatusUnauthorized, wantBody: `{"code":"invalid_token","message":"missing bearer token"}`},
		{name: "refused token", call: &call{authErr: service.ErrUnauthorized}, wantStatus: http.StatusUnauthorized, wantBody: `{"code":"invalid_token","message":"unknown or revoked agent token"}`},
		{name: "authentication fails", call: &call{authErr: errDB}, wantStatus: http.StatusInternalServerError, wantBody: `{"code":"internal_error"}`},
		{name: "not gzip", encoding: "-", call: &call{}, wantStatus: http.StatusUnsupportedMediaType, wantBody: `{"code":"invalid_encoding","message":"the body must be compressed with gzip"}`},
		{name: "corrupt gzip", body: []byte("not gzip"), call: &call{}, wantStatus: http.StatusBadRequest},
		{name: "truncated gzip", body: gzipped(t, body)[:20], call: &call{}, wantStatus: http.StatusBadRequest},
		{name: "compressed too large", body: bytes.Repeat([]byte("x"), 3*int(config.KB)), call: &call{}, wantStatus: http.StatusRequestEntityTooLarge, wantBody: `{"code":"payload_too_large","message":"compressed body exceeds 2048 bytes"}`},
		{name: "decompressed too large", body: gzipped(t, bytes.Repeat([]byte(" "), 5*int(config.KB))), call: &call{}, wantStatus: http.StatusRequestEntityTooLarge, wantBody: `{"code":"payload_too_large","message":"decompressed body exceeds 4096 bytes"}`},
		{name: "not json", body: gzipped(t, []byte("{")), call: &call{}, wantStatus: http.StatusUnprocessableEntity},
		{name: "invalid snapshot", call: &call{ingest: true, ingestErr: service.ErrInvalid}, wantStatus: http.StatusUnprocessableEntity, wantBody: `{"code":"invalid_schema","message":"invalid input"}`},
		{name: "namespace mismatch", call: &call{ingest: true, ingestErr: service.ErrNamespaceMismatch}, wantStatus: http.StatusForbidden, wantBody: `{"code":"namespace_mismatch","message":"namespace mismatch"}`},
		{name: "ingestion fails", call: &call{ingest: true, ingestErr: errDB}, wantStatus: http.StatusInternalServerError, wantBody: `{"code":"internal_error"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mockService.NewMockSnapshotService(gomock.NewController(t))
			if tt.call != nil {
				var got *model.Agent
				if tt.call.authErr == nil {
					got = agent
				}
				svc.EXPECT().Authenticate(gomock.Any(), "lacis_at_secret").Return(got, tt.call.authErr)
				if tt.call.ingest {
					svc.EXPECT().Ingest(gomock.Any(), agent, gomock.Any(), json.RawMessage(body)).
						DoAndReturn(func(_ any, _ *model.Agent, s *snapshot.Snapshot, _ json.RawMessage) (service.IngestResult, error) {
							assert.Equal(t, "acme-site-prod-1", s.Host.Hostname)
							return tt.call.result, tt.call.ingestErr
						})
				}
			}
			e := echo.New()
			NewHandler(discard, cfg, nil, svc).Register(e.Group("/lacis"))

			reqBody := tt.body
			if reqBody == nil {
				reqBody = gzipped(t, body)
			}
			req := httptest.NewRequest(http.MethodPost, "/lacis/"+api.PathSnapshot, bytes.NewReader(reqBody))
			if tt.authorization != "-" {
				req.Header.Set(api.HeaderAuthorization, "Bearer lacis_at_secret")
			}
			if tt.encoding != "-" {
				req.Header.Set(api.HeaderContentEncoding, api.EncodingGzip)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, rec.Body.String())
			}
		})
	}
}

// TestSnapshot_ChunkedTooLarge checks the compressed limit without a
// Content-Length.
func TestSnapshot_ChunkedTooLarge(t *testing.T) {
	svc := mockService.NewMockSnapshotService(gomock.NewController(t))
	svc.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(&model.Agent{}, nil)
	cfg := config.DefaultConfig().AgentAPI
	cfg.MaxBodyCompressed = config.KB
	e := echo.New()
	NewHandler(discard, cfg, nil, svc).Register(e.Group(""))

	random := make([]byte, 4*config.KB)
	_, _ = rand.Read(random) // incompressible
	req := httptest.NewRequest(http.MethodPost, "/"+api.PathSnapshot, io.NopCloser(bytes.NewReader(gzipped(t, random))))
	req.ContentLength = -1
	req.Header.Set(api.HeaderAuthorization, "Bearer lacis_at_secret")
	req.Header.Set(api.HeaderContentEncoding, api.EncodingGzip)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.True(t, strings.Contains(rec.Body.String(), "compressed body exceeds 1024 bytes"), rec.Body.String())
}
