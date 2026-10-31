package cli

import (
	stdContext "context"
	"fmt"
	"net"

	"github.com/alexandreh2ag/lacis/auth/openid"
	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database"
	lacisHttp "github.com/alexandreh2ag/lacis/http"
	authRoute "github.com/alexandreh2ag/lacis/http/route/auth"
	"github.com/alexandreh2ag/lacis/queue"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/alexandreh2ag/lacis/web"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

// NewServices opens the database, migrates it when database.migrate_on_start
// is set, starts the workers and the scheduled tasks when workers.enabled is
// set, and builds the
// services served by start, with the function releasing them; tests
// replace it.
var NewServices = func(stdCtx stdContext.Context, ctx *context.Context) (*lacisHttp.Services, func(), error) {
	if ctx.Config.Database.MigrateOnStart {
		if err := database.NewMigrator(ctx.Config.Database.DSN, ctx.Logger).Migrate(stdCtx); err != nil {
			return nil, nil, err
		}
	}
	db, err := database.Open(ctx.Config.Database, ctx.Logger)
	if err != nil {
		return nil, nil, err
	}
	closeDB := func() { _ = database.Close(db) }
	jobs, stopJobs, err := StartJobs(stdCtx, ctx, db, ctx.Config.Workers.Enabled)
	if err != nil {
		closeDB()
		return nil, nil, err
	}
	release := func() {
		stopJobs()
		closeDB()
	}

	if err := bootstrapAdmin(stdCtx, ctx, service.NewUserService(ctx, repository.NewTransactor(db))); err != nil {
		release()
		return nil, nil, err
	}

	repos := repository.NewRepositories(db)
	services := &lacisHttp.Services{
		Agent:     service.NewAgentService(ctx, repos.EnrollmentToken, repos.Agent),
		Snapshot:  service.NewSnapshotService(ctx, repository.NewTransactor(db), repos.Agent, jobs),
		Auth:      service.NewAuthService(ctx, repository.NewTransactor(db)),
		Namespace: service.NewNamespaceService(ctx, repository.NewTransactor(db)),
		History:   service.NewHistoryService(ctx, repository.NewTransactor(db)),
		Metadata:  service.NewMetadataService(ctx, repository.NewTransactor(db), jobs),
		User:      service.NewUserService(ctx, repository.NewTransactor(db)),
		Role:      service.NewRoleService(ctx, repository.NewTransactor(db)),
		Token:     service.NewEnrollmentTokenService(ctx, repos.EnrollmentToken, repos.Agent),
		TechIcon:  service.NewTechIconService(ctx, repository.NewTransactor(db)),
		Task:      service.NewTaskService(ctx, jobs),
		UI:        web.Dist(),
	}
	if ctx.Config.Auth.OIDC.Enabled {
		services.OIDC = openid.New(ctx.Config.Auth.OIDC, authRoute.CallbackURL(ctx.Config.Listen))
	}
	return services, release, nil
}

// StartJobs builds the job queue on db; with work, it works the jobs and
// runs the scheduled tasks until the returned function stops them,
// gracefully. Without, the queue only inserts.
func StartJobs(stdCtx stdContext.Context, ctx *context.Context, db *gorm.DB, work bool) (*queue.Client, func(), error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, err
	}
	var workers *queue.Workers
	if work {
		linker, err := service.NewLinkService(ctx, repository.NewTransactor(db))
		if err != nil {
			return nil, nil, err
		}
		workers = &queue.Workers{Normalizer: service.NewNormalizeService(ctx, repository.NewTransactor(db)), Linker: linker}
	}
	jobs, err := queue.New(sqlDB, ctx.Config.Workers, ctx.Logger, workers)
	if err != nil {
		return nil, nil, err
	}
	if !work {
		return jobs, func() {}, nil
	}
	// The maintainer deletes hosts, which enqueues the recomputation of
	// their links through the client.
	workers.Maintainer = service.NewMaintenanceService(ctx, repository.NewTransactor(db),
		service.NewMetadataService(ctx, repository.NewTransactor(db), jobs))
	// Not cancelled with stdCtx: the returned function stops the workers
	// gracefully.
	if err := jobs.Start(stdContext.WithoutCancel(stdCtx)); err != nil {
		return nil, nil, fmt.Errorf("start workers: %w", err)
	}
	ctx.Logger.Info("workers started", "concurrency", ctx.Config.Workers.Concurrency)
	return jobs, func() {
		stopCtx, cancel := stdContext.WithTimeout(stdContext.Background(), lacisHttp.GracefulTimeout)
		defer cancel()
		if err := jobs.Stop(stopCtx); err != nil {
			ctx.Logger.Warn("stop workers", "error", err)
		}
	}, nil
}

// bootstrapAdmin creates the admin of auth.local.bootstrap_admin_*, unless
// a user already has its username.
func bootstrapAdmin(stdCtx stdContext.Context, ctx *context.Context, users service.UserService) error {
	local := ctx.Config.Auth.Local
	if !local.Enabled || local.BootstrapAdminUsername == "" {
		return nil
	}
	created, err := users.Bootstrap(stdCtx, local.BootstrapAdminUsername, local.BootstrapAdminPassword)
	if err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	if created {
		ctx.Logger.Info("bootstrap admin created", "username", local.BootstrapAdminUsername)
	}
	return nil
}

func GetStartCmd(ctx *context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Serve the API and the front",
		RunE:  GetStartRunFn(ctx, nil),
	}
}

// GetStartRunFn serves until SIGINT or SIGTERM. onListen, when set, receives
// the address listened on.
func GetStartRunFn(ctx *context.Context, onListen func(net.Addr)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		stdCtx, cancel := stdContext.WithCancel(cmd.Context())
		defer cancel()
		go func() {
			select {
			case sig := <-ctx.Signal():
				ctx.Logger.Info("shutting down", "signal", sig.String())
				cancel()
			case <-stdCtx.Done():
			}
		}()
		services, release, err := NewServices(stdCtx, ctx)
		if err != nil {
			return err
		}
		defer release()
		return lacisHttp.Start(stdCtx, ctx, lacisHttp.CreateServerHTTP(ctx, services), onListen)
	}
}
