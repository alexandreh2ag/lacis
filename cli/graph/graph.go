// Package graph holds the "graph" subcommands of lacis-server.
package graph

import (
	"fmt"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/alexandreh2ag/lacis/topology"
	"github.com/spf13/cobra"
)

const (
	FlagNamespace = "namespace"
	FlagFormat    = "format"
	FlagView      = "view"
	FlagProject   = "project"
	FlagEnv       = "env"
	FlagHost      = "host"
	FlagOutput    = "output"
	FlagLabels    = "labels"
	FlagLayout    = "layout"
)

// NewService builds the service used by "graph export", and the function
// releasing it; tests replace it.
var NewService = func(ctx *context.Context) (service.GraphService, func(), error) {
	db, err := database.Open(ctx.Config.Database, ctx.Logger)
	if err != nil {
		return nil, nil, err
	}
	return service.NewGraphService(ctx, repository.NewTransactor(db)), func() { _ = database.Close(db) }, nil
}

func GetGraphCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Graph commands",
	}
	cmd.AddCommand(GetExportCmd(ctx))
	return cmd
}

func GetExportCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the graph of a namespace",
		Long: "Export the graph of a namespace, as the links were last computed: Mermaid and DOT to draw it, " +
			"JSON as the API serves it. The filters keep the nodes of their projects, envs and hosts, and the nodes " +
			"they are linked to. The views: service draws every app, service and webserver; app folds the webserver " +
			"and php-fpm of a host into its apps; host stacks the similar hosts; project draws a node per project and env.",
		Example: "  lacis-server graph export --namespace acme > acme.mmd\n" +
			"  lacis-server graph export --namespace acme --env production --format dot | dot -Tsvg > prod.svg\n" +
			"  lacis-server graph export --namespace acme --layout elk --labels > acme.mmd\n" +
			"  lacis-server graph export --namespace acme --view host --format json -o acme.json",
		Args: cobra.NoArgs,
		RunE: GetExportRunFn(ctx),
	}
	cmd.Flags().StringP(FlagNamespace, "n", "", "slug of the namespace")
	cmd.Flags().StringP(FlagFormat, "f", string(topology.FormatMermaid), "output format: "+join(topology.Formats))
	cmd.Flags().String(FlagView, string(topology.LevelService), "level of the graph: "+join(topology.Levels))
	cmd.Flags().StringSlice(FlagProject, nil, "keep these projects (slugs, repeatable)")
	cmd.Flags().StringSlice(FlagEnv, nil, "keep these envs (slugs, repeatable)")
	cmd.Flags().StringSlice(FlagHost, nil, "keep these hosts (hostnames, repeatable); a host named leaves its stack")
	cmd.Flags().StringP(FlagOutput, "o", "", "file to write, stdout by default")
	cmd.Flags().Bool(FlagLabels, false, "write on each edge its keys or domains")
	cmd.Flags().String(FlagLayout, string(topology.LayoutDagre), "layout engine of mermaid: "+join(topology.Layouts)+"; elk suits the large graphs")
	_ = cmd.MarkFlagRequired(FlagNamespace)
	return cmd
}

func join[T ~string](values []T) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return strings.Join(out, ", ")
}

func GetExportRunFn(ctx *context.Context) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		namespace, _ := cmd.Flags().GetString(FlagNamespace)
		format, _ := cmd.Flags().GetString(FlagFormat)
		level, _ := cmd.Flags().GetString(FlagView)
		projects, _ := cmd.Flags().GetStringSlice(FlagProject)
		envs, _ := cmd.Flags().GetStringSlice(FlagEnv)
		hosts, _ := cmd.Flags().GetStringSlice(FlagHost)
		output, _ := cmd.Flags().GetString(FlagOutput)
		labels, _ := cmd.Flags().GetBool(FlagLabels)
		layout, _ := cmd.Flags().GetString(FlagLayout)
		if !slices.Contains(topology.Formats, topology.Format(format)) {
			return fmt.Errorf("unknown format %q, expected one of: %s", format, join(topology.Formats))
		}
		if !slices.Contains(topology.Levels, topology.Level(level)) {
			return fmt.Errorf("unknown view %q, expected one of: %s", level, join(topology.Levels))
		}
		if !slices.Contains(topology.Layouts, topology.Layout(layout)) {
			return fmt.Errorf("unknown layout %q, expected one of: %s", layout, join(topology.Layouts))
		}

		svc, release, err := NewService(ctx)
		if err != nil {
			return err
		}
		defer release()
		g, err := svc.Graph(cmd.Context(), namespace, topology.Filter{Level: topology.Level(level), Projects: projects, Envs: envs, Hosts: hosts})
		if err != nil {
			return err
		}
		w := cmd.OutOrStdout()
		if output != "" {
			f, err := ctx.Fs.Create(output)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			w = f
		}
		if err := topology.Render(w, g, topology.Format(format), topology.Options{Labels: labels, Layout: topology.Layout(layout)}); err != nil {
			return err
		}
		if output != "" {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%d nodes, %d edges written to %s\n", len(g.Nodes), len(g.Edges), output)
		}
		return nil
	}
}
