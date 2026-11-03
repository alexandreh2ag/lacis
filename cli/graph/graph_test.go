package graph

import (
	"bytes"
	"errors"
	"testing"

	"github.com/alexandreh2ag/lacis/context"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/alexandreh2ag/lacis/topology"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func withService(t *testing.T, svc service.GraphService, err error) *bool {
	released := false
	previous := NewService
	NewService = func(*context.Context) (service.GraphService, func(), error) {
		return svc, func() { released = true }, err
	}
	t.Cleanup(func() { NewService = previous })
	return &released
}

func run(ctx *context.Context, args ...string) (string, string, error) {
	cmd := GetGraphCmd(ctx)
	cmd.SilenceUsage = true
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

var graph = &topology.Graph{Namespace: "acme", Level: topology.LevelHost,
	Nodes: []topology.Node{{ID: "host:a", Kind: "host", Label: "a"}, {ID: "host:b", Kind: "host", Label: "b"}},
	Edges: []topology.Edge{{From: "host:a", To: "host:b", Confidence: "exact"}}}

func TestExportCmd(t *testing.T) {
	svc := mockService.NewMockGraphService(gomock.NewController(t))
	svc.EXPECT().Graph(gomock.Any(), "acme", topology.Filter{Level: topology.LevelHost, Projects: []string{"shop", "crm"}, Envs: []string{"production"}, Hosts: []string{"web-1"}}).
		Return(graph, nil)
	released := withService(t, svc, nil)

	out, _, err := run(context.TestContext(nil), "export", "-n", "acme", "--view", "host", "--project", "shop,crm", "--env", "production", "--host", "web-1", "-f", "dot")
	require.NoError(t, err)
	assert.Contains(t, out, "digraph \"acme\" {")
	assert.Contains(t, out, "  n0 -> n1 [color=\"#2563eb\"];\n")
	assert.True(t, *released)
}

func TestExportCmd_Output(t *testing.T) {
	svc := mockService.NewMockGraphService(gomock.NewController(t))
	svc.EXPECT().Graph(gomock.Any(), "acme", topology.Filter{Level: topology.LevelService, Projects: []string{}, Envs: []string{}, Hosts: []string{}}).Return(graph, nil)
	withService(t, svc, nil)
	ctx := context.TestContext(nil)

	out, errOut, err := run(ctx, "export", "--namespace", "acme", "-o", "/acme.mmd")
	require.NoError(t, err)
	assert.Empty(t, out)
	assert.Equal(t, "2 nodes, 1 edges written to /acme.mmd\n", errOut)
	written, err := afero.ReadFile(ctx.Fs, "/acme.mmd")
	require.NoError(t, err)
	assert.Contains(t, string(written), "flowchart LR\n")
}

func TestExportCmd_Errors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		svcErr  error
		graph   error
		wantErr string
	}{
		{name: "no namespace", args: []string{"export"}, wantErr: `required flag(s) "namespace" not set`},
		{name: "unknown format", args: []string{"export", "-n", "acme", "-f", "png"}, wantErr: `unknown format "png", expected one of: mermaid, dot, json`},
		{name: "unknown layout", args: []string{"export", "-n", "acme", "--layout", "neato"}, wantErr: `unknown layout "neato", expected one of: dagre, elk`},
		{name: "unknown view", args: []string{"export", "-n", "acme", "--view", "rack"}, wantErr: `unknown view "rack", expected one of: service, host, project, app`},
		{name: "service unavailable", args: []string{"export", "-n", "acme"}, svcErr: errors.New("database down"), wantErr: "database down"},
		{name: "unknown namespace", args: []string{"export", "-n", "nope"}, graph: service.ErrNamespaceNotFound, wantErr: "namespace not found"},
		{name: "unwritable output", args: []string{"export", "-n", "acme", "-o", "/ro/acme.mmd"}, wantErr: "operation not permitted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mockService.NewMockGraphService(gomock.NewController(t))
			if tt.svcErr == nil {
				svc.EXPECT().Graph(gomock.Any(), gomock.Any(), gomock.Any()).Return(graph, tt.graph).MaxTimes(1)
			}
			withService(t, svc, tt.svcErr)
			ctx := context.TestContext(nil)
			ctx.Fs = afero.NewReadOnlyFs(afero.NewMemMapFs())
			_, _, err := run(ctx, tt.args...)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestNewService(t *testing.T) {
	ctx := context.TestContext(nil)
	ctx.Config.Database.DSN = "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"
	ctx.Config.Database.MaxOpenConns = 1
	_, _, err := NewService(ctx)
	assert.ErrorContains(t, err, "open database")
}

func TestExportCmd_LabelsAndLayout(t *testing.T) {
	labelled := &topology.Graph{Namespace: "acme", Level: topology.LevelHost, Nodes: graph.Nodes,
		Edges: []topology.Edge{{From: "host:a", To: "host:b", Confidence: "exact", Labels: []string{"DATABASE_URL"}}}}
	svc := mockService.NewMockGraphService(gomock.NewController(t))
	svc.EXPECT().Graph(gomock.Any(), "acme", gomock.Any()).Return(labelled, nil).Times(2)
	withService(t, svc, nil)

	out, _, err := run(context.TestContext(nil), "export", "-n", "acme")
	require.NoError(t, err)
	assert.Contains(t, out, "  n0 --> n1\n", "no label by default")

	out, _, err = run(context.TestContext(nil), "export", "-n", "acme", "--labels", "--layout", "elk")
	require.NoError(t, err)
	assert.Contains(t, out, "---\nconfig:\n  layout: elk\n---\n")
	assert.Contains(t, out, `  n0 -->|"DATABASE_URL"| n1`+"\n")
}
