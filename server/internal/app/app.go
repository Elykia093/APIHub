package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/elykia/apihub/server/ent"
	"github.com/elykia/apihub/server/internal/adapter"
	"github.com/elykia/apihub/server/internal/api"
	"github.com/elykia/apihub/server/internal/config"
	"github.com/elykia/apihub/server/internal/cryptoutil"
	"github.com/elykia/apihub/server/internal/migrate"
	"github.com/elykia/apihub/server/internal/netclient"
	"github.com/elykia/apihub/server/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type Runtime struct {
	Server    *http.Server
	DB        *sql.DB
	Ent       *ent.Client
	Scheduler *service.Scheduler
	HTTP      *netclient.Client
}

func Build(ctx context.Context, cfg config.Config, logger *slog.Logger, web fs.FS) (*Runtime, error) {
	pgxConfig, err := pgx.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}
	pgxConfig.ConnectTimeout = cfg.DatabaseConnectionTimeout
	pgxConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(cfg.DatabaseStatementTimeout.Milliseconds(), 10)
	db := stdlib.OpenDB(*pgxConfig)
	db.SetMaxOpenConns(cfg.DatabasePoolMax)
	db.SetMaxIdleConns(cfg.DatabasePoolMax)
	db.SetConnMaxIdleTime(cfg.DatabaseIdleTimeout)
	db.SetConnMaxLifetime(30 * time.Minute)
	connectionCtx, cancel := context.WithTimeout(ctx, cfg.DatabaseConnectionTimeout)
	defer cancel()
	if err := db.PingContext(connectionCtx); err != nil {
		connectErr := fmt.Errorf("connect PostgreSQL: %w", err)
		if closeErr := db.Close(); closeErr != nil {
			return nil, errors.Join(connectErr, fmt.Errorf("close PostgreSQL after failed connection: %w", closeErr))
		}
		return nil, connectErr
	}
	if err := migrate.Run(ctx, db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close PostgreSQL after failed migration: %w", closeErr))
		}
		return nil, err
	}
	driver := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(driver))
	vault := cryptoutil.NewVault(cfg.AppSecret)
	httpClient := netclient.New(netclient.Options{Timeout: cfg.HTTPTimeout, MaxResponseBytes: cfg.MaxResponseBytes, AllowPrivateSites: cfg.AllowPrivateSites, AllowInsecureHTTP: cfg.AllowInsecureHTTP})
	registry := adapter.NewRegistry(vault, adapter.NewNewAPI(httpClient), adapter.NewSub2API(httpClient), adapter.NewZenAPI(httpClient))
	detector := adapter.NewDetector(httpClient)
	sites := service.NewSiteService(client, registry, detector, vault, cfg.AllowPrivateSites, cfg.AllowInsecureHTTP)
	checkins := service.NewCheckinService(client, registry, logger)
	announcements := service.NewAnnouncementService(client, registry, logger)
	scheduler := service.NewScheduler(ctx, client, checkins, announcements, logger)
	if err := scheduler.Reload(ctx); err != nil {
		httpClient.Close()
		if closeErr := client.Close(); closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close PostgreSQL after failed scheduler reload: %w", closeErr))
		}
		return nil, err
	}
	router := api.NewRouter(api.Dependencies{Config: cfg, DB: db, Sites: sites, Checkins: checkins, Announcements: announcements, Scheduler: scheduler, Adapters: registry, Logger: logger, Web: web})
	server := &http.Server{Addr: cfg.Address(), Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	return &Runtime{Server: server, DB: db, Ent: client, Scheduler: scheduler, HTTP: httpClient}, nil
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	serverErr := r.Server.Shutdown(ctx)
	schedulerErr := r.Scheduler.Stop(ctx)
	r.HTTP.Close()
	entErr := r.Ent.Close()
	return errors.Join(serverErr, schedulerErr, entErr)
}
