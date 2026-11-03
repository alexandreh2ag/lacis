package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/alexandreh2ag/lacis/context"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func withService(t *testing.T, svc service.ImportService, err error) *bool {
	released := false
	previous := NewService
	NewService = func(*context.Context) (service.ImportService, func(), error) {
		return svc, func() { released = true }, err
	}
	t.Cleanup(func() { NewService = previous })
	return &released
}

func run(ctx *context.Context, args ...string) (string, error) {
	cmd := GetSnapshotCmd(ctx)
	cmd.SilenceUsage = true
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestImportCmd(t *testing.T) {
	ctx := context.TestContext(nil)
	require.NoError(t, afero.WriteFile(ctx.Fs, "/snaps/b.json", []byte(`{"b":1}`), 0o644))
	require.NoError(t, afero.WriteFile(ctx.Fs, "/snaps/a.json", []byte(`{"a":1}`), 0o644))
	require.NoError(t, afero.WriteFile(ctx.Fs, "/snaps/notes.txt", []byte("x"), 0o644))
	require.NoError(t, afero.WriteFile(ctx.Fs, "/one.json", []byte(`{"c":1}`), 0o644))

	svc := mockService.NewMockImportService(gomock.NewController(t))
	gomock.InOrder(
		svc.EXPECT().Import(gomock.Any(), json.RawMessage(`{"a":1}`)).Return(&service.ImportResult{Namespace: "acme", Hostname: "a", Result: service.Accepted, AgentCreated: true}, nil),
		svc.EXPECT().Import(gomock.Any(), json.RawMessage(`{"b":1}`)).Return(nil, service.ErrInvalid),
		svc.EXPECT().Import(gomock.Any(), json.RawMessage(`{"c":1}`)).Return(&service.ImportResult{Namespace: "acme", Hostname: "c", Result: service.Unchanged}, nil),
	)
	released := withService(t, svc, nil)

	out, err := run(ctx, "import", "/snaps", "/one.json")
	assert.EqualError(t, err, "/snaps/b.json: invalid input", "a failed file does not stop the others")
	assert.Equal(t, "a (acme): accepted, agent created\nc (acme): unchanged\n", out)
	assert.True(t, *released)
}

func TestImportCmd_Errors(t *testing.T) {
	t.Run("no argument", func(t *testing.T) {
		_, err := run(context.TestContext(nil), "import")
		assert.ErrorContains(t, err, "requires at least 1 arg")
	})
	t.Run("missing file", func(t *testing.T) {
		_, err := run(context.TestContext(nil), "import", "/missing.json")
		assert.ErrorContains(t, err, "/missing.json")
	})
	t.Run("service unavailable", func(t *testing.T) {
		withService(t, nil, errors.New("database down"))
		ctx := context.TestContext(nil)
		require.NoError(t, afero.WriteFile(ctx.Fs, "/one.json", []byte(`{}`), 0o644))
		_, err := run(ctx, "import", "/one.json")
		assert.ErrorContains(t, err, "database down")
	})
}

func TestNewService(t *testing.T) {
	ctx := context.TestContext(nil)
	ctx.Config.Database.DSN = "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"
	ctx.Config.Database.MaxOpenConns = 1
	_, _, err := NewService(ctx)
	assert.ErrorContains(t, err, "open database")
}

func TestNormalizeCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mockService.NewMockNormalizeService(ctrl)
	svc.EXPECT().NormalizeAll(gomock.Any()).Return(24, errors.New("snapshot 7: decode"))
	links := mockService.NewMockLinkService(ctrl)
	links.EXPECT().RecomputeAll(gomock.Any()).Return(86, errors.New("namespace acme: db"))
	released := false
	previous := NewNormalizer
	NewNormalizer = func(*context.Context) (service.NormalizeService, service.LinkService, func(), error) {
		return svc, links, func() { released = true }, nil
	}
	t.Cleanup(func() { NewNormalizer = previous })

	out, err := run(context.TestContext(nil), "normalize")
	assert.EqualError(t, err, "snapshot 7: decode\nnamespace acme: db", "the links are computed despite a failed snapshot")
	assert.Equal(t, "24 snapshots normalized\n86 links computed\n", out)
	assert.True(t, released)

	NewNormalizer = func(*context.Context) (service.NormalizeService, service.LinkService, func(), error) {
		return nil, nil, nil, errors.New("database down")
	}
	_, err = run(context.TestContext(nil), "normalize")
	assert.EqualError(t, err, "database down")

	NewNormalizer = previous
	ctx := context.TestContext(nil)
	ctx.Config.Database.DSN = "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"
	ctx.Config.Database.MaxOpenConns = 1
	_, _, _, err = NewNormalizer(ctx)
	assert.ErrorContains(t, err, "open database")
}
