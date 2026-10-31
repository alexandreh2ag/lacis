//go:build integration

package cli

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/common/api"
	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database/dbtest"
	"github.com/alexandreh2ag/lacis/hash"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/google/uuid"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStartCmd_Enroll runs start on a real database and enrolls an agent.
func TestStartCmd_Enroll(t *testing.T) {
	db := dbtest.Open(t, "cli")
	ctx := context.TestContext(nil)
	created, err := service.NewEnrollmentTokenService(ctx, repository.NewEnrollmentTokenRepository(db), repository.NewAgentRepository(db)).
		Create(t.Context(), service.CreateEnrollmentTokenInput{})
	require.NoError(t, err)

	yaml := strings.Replace(validYAML, "postgres://lacis@db/lacis", dbtest.DSN(t, "cli"), 1)
	yaml += "  local:\n    enabled: true\n    bootstrap_admin_username: admin\n    bootstrap_admin_password: s3cret-password\n"
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/server.yaml", []byte(yaml), 0o600))
	addrs := make(chan net.Addr, 1)
	cmd := GetRootCmd(ctx)
	for _, c := range cmd.Commands() {
		if c.Name() == "start" {
			c.RunE = GetStartRunFn(ctx, func(a net.Addr) { addrs <- a })
		}
	}
	errs := make(chan error, 1)
	go func() {
		_, err := runCmd(cmd, "start")
		errs <- err
	}()
	var addr net.Addr
	select {
	case addr = <-addrs:
	case err := <-errs:
		t.Fatalf("start failed: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server did not start in time")
	}

	enroll := func(secret string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/%s", addr, api.PathEnroll), nil)
		require.NoError(t, err)
		req.Header.Set(api.HeaderAuthorization, api.BearerPrefix+secret)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	resp := enroll(created.Secret)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	got := api.EnrollResponse{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	agent, err := repository.NewAgentRepository(db).FindByID(t.Context(), uuid.MustParse(got.AgentID))
	require.NoError(t, err)
	assert.Equal(t, hash.Token(got.AgentToken), agent.TokenHash)
	assert.Equal(t, created.Token.ID, agent.EnrollmentTokenID)

	assert.Equal(t, http.StatusUnauthorized, enroll("lacis_et_unknown").StatusCode)

	admin, err := repository.NewUserRepository(db).FindByUsername(t.Context(), "admin")
	require.NoError(t, err, "the bootstrap admin is created")
	ok, err := hash.VerifyPassword("s3cret-password", *admin.PasswordHash)
	require.NoError(t, err)
	assert.True(t, ok)

	// Snapshots: stored, then unchanged.
	snap := snapshot.New(snapshot.AgentInfo{Version: "1.2.0"}, labels.Labels{Namespace: "beta", Project: "site", Env: "prod"}, "beta-site-prod-1")
	snap.CollectedAt = time.Now().UTC()
	body, err := json.Marshal(snap)
	require.NoError(t, err)
	gzipped := &bytes.Buffer{}
	gz := gzip.NewWriter(gzipped)
	_, _ = gz.Write(body)
	require.NoError(t, gz.Close())
	send := func() *http.Response {
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/%s", addr, api.PathSnapshot), bytes.NewReader(gzipped.Bytes()))
		require.NoError(t, err)
		req.Header.Set(api.HeaderAuthorization, api.BearerPrefix+got.AgentToken)
		req.Header.Set(api.HeaderContentEncoding, api.EncodingGzip)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}
	assert.Equal(t, http.StatusAccepted, send().StatusCode)
	assert.Equal(t, http.StatusOK, send().StatusCode)

	repos := repository.NewRepositories(db)
	namespace, err := repos.Namespace.FindBySlug(t.Context(), "beta")
	require.NoError(t, err, "the namespace is created with the host")
	host, err := repos.Host.FindByAgentID(t.Context(), agent.ID)
	require.NoError(t, err)
	assert.Equal(t, namespace.ID, host.NamespaceID)
	assert.Equal(t, "beta-site-prod-1", host.Hostname)
	stored, err := repos.Snapshot.FindLatestByHost(t.Context(), host.ID)
	require.NoError(t, err)
	assert.JSONEq(t, string(body), string(stored.Payload))
	var count int64
	require.NoError(t, db.Model(&model.Snapshot{}).Where("host_id = ?", host.ID).Count(&count).Error)
	assert.Equal(t, int64(1), count, "the unchanged snapshot is not stored")
	agent, err = repos.Agent.FindByID(t.Context(), agent.ID)
	require.NoError(t, err)
	assert.NotNil(t, agent.LastSeenAt)
	assert.Eventually(t, func() bool {
		var state string
		_ = db.Raw(`select state from river_job where kind = 'normalize_snapshot' and (args->>'snapshot_id')::bigint = ?`, stored.ID).Row().Scan(&state)
		return state == "completed"
	}, 10*time.Second, 100*time.Millisecond, "the normalization job is worked")

	ctx.Signal() <- syscall.SIGTERM
	select {
	case err := <-errs:
		assert.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("server did not stop")
	}
}
