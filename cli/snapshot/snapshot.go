// Package snapshot holds the "snapshot" subcommands of lacis-server.
package snapshot

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database"
	"github.com/alexandreh2ag/lacis/queue"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

// NewNormalizer builds the normalization and the linking used by
// "snapshot normalize", and the function releasing them; tests replace it.
var NewNormalizer = func(ctx *context.Context) (service.NormalizeService, service.LinkService, func(), error) {
	db, err := database.Open(ctx.Config.Database, ctx.Logger)
	if err != nil {
		return nil, nil, nil, err
	}
	release := func() { _ = database.Close(db) }
	tx := repository.NewTransactor(db)
	links, err := service.NewLinkService(ctx, tx)
	if err != nil {
		release()
		return nil, nil, nil, err
	}
	return service.NewNormalizeService(ctx, tx), links, release, nil
}

// NewService builds the service used by "snapshot import", and the function
// releasing it; tests replace it.
var NewService = func(ctx *context.Context) (service.ImportService, func(), error) {
	db, err := database.Open(ctx.Config.Database, ctx.Logger)
	if err != nil {
		return nil, nil, err
	}
	release := func() { _ = database.Close(db) }
	sqlDB, err := db.DB()
	if err != nil {
		release()
		return nil, nil, err
	}
	// Insert only: the workers of start normalize the snapshots.
	jobs, err := queue.New(sqlDB, ctx.Config.Workers, ctx.Logger, nil)
	if err != nil {
		release()
		return nil, nil, err
	}
	repos := repository.NewRepositories(db)
	snapshots := service.NewSnapshotService(ctx, repository.NewTransactor(db), repos.Agent, jobs)
	return service.NewImportService(ctx, repos, snapshots), release, nil
}

func GetSnapshotCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Snapshot commands",
	}
	cmd.AddCommand(GetImportCmd(ctx), GetNormalizeCmd(ctx))
	return cmd
}

func GetImportCmd(ctx *context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "import <file or directory>...",
		Short: "Import snapshots without agent, for development and tests",
		Long: "Ingest snapshot files as if their agents sent them: an agent is created on the first import of " +
			"a host, and reused after. A directory gives its *.json files. The workers of start normalize them.",
		Example: "  lacis-server snapshot import testdata/snapshots\n" +
			"  lacis-server snapshot import /tmp/snapshot.json",
		Args: cobra.MinimumNArgs(1),
		RunE: GetImportRunFn(ctx),
	}
}

func GetImportRunFn(ctx *context.Context) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		files, err := snapshotFiles(ctx.Fs, args)
		if err != nil {
			return err
		}
		svc, release, err := NewService(ctx)
		if err != nil {
			return err
		}
		defer release()

		var errs []error
		for _, file := range files {
			raw, err := afero.ReadFile(ctx.Fs, file)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			res, err := svc.Import(cmd.Context(), raw)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", file, err))
				continue
			}
			status := "accepted"
			if res.Result == service.Unchanged {
				status = "unchanged"
			}
			if res.AgentCreated {
				status += ", agent created"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s (%s): %s\n", res.Hostname, res.Namespace, status)
		}
		return errors.Join(errs...)
	}
}

func GetNormalizeCmd(ctx *context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "normalize",
		Short: "Normalize again the latest snapshot of every host, and compute the links",
		Long: "Normalize again, in this process, the latest snapshot of every host, then compute the links of " +
			"every namespace: after a change of the normalization or of the linking, or to normalize imported " +
			"snapshots without running the workers.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, links, release, err := NewNormalizer(ctx)
			if err != nil {
				return err
			}
			defer release()
			done, errNormalize := svc.NormalizeAll(cmd.Context())
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%d snapshots normalized\n", done)
			stored, errLinks := links.RecomputeAll(cmd.Context())
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%d links computed\n", stored)
			return errors.Join(errNormalize, errLinks)
		},
	}
}

// snapshotFiles expands the directories of args into their *.json files.
func snapshotFiles(fsys afero.Fs, args []string) ([]string, error) {
	var files []string
	for _, arg := range args {
		info, err := fsys.Stat(arg)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, arg)
			continue
		}
		matches, _ := afero.Glob(fsys, filepath.Join(arg, "*.json")) // only ErrBadPattern
		slices.Sort(matches)
		files = append(files, matches...)
	}
	return files, nil
}
