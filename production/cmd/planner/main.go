package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/GM-Access/gm-vector/production/internal/planner"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if len(os.Args) < 2 {
		slog.Error("usage: planner init|release-info|migrate|api|worker|seed-calendar|import|recover-user")
		os.Exit(2)
	}
	mode := os.Args[1]
	// Packaging probes require neither installation secrets nor a database.
	if mode == "release-info" {
		if len(os.Args) != 2 {
			slog.Error("release-info takes no arguments")
			os.Exit(2)
		}
		_, release, err := planner.RuntimeReleaseTrust()
		if err != nil {
			slog.Error("release trust unavailable")
			os.Exit(1)
		}
		if err = json.NewEncoder(os.Stdout).Encode(release); err != nil {
			os.Exit(1)
		}
		return
	}
	flags := flag.NewFlagSet(mode, flag.ExitOnError)
	config := flags.String("config", "/run/secrets/config.json", "configuration file")
	addr := flags.String("listen", ":8080", "API bind address")
	file := flags.String("file", "", "input JSON file")
	outputStdout := flags.Bool("output-stdout", false, "capture recovery link on stdout for a local installer; never log it")
	username := flags.String("username", "", "existing local administrator to recover")
	directory := flags.String("dir", "", "new private configuration directory")
	origin := flags.String("origin", "https://localhost:9443", "public HTTPS origin")
	flags.Parse(os.Args[2:])
	if *outputStdout && mode != "recover-user" {
		slog.Error("--output-stdout is only valid for recover-user")
		os.Exit(2)
	}
	if mode == "init" {
		if *directory == "" {
			slog.Error("--dir is required")
			os.Exit(2)
		}
		if e := planner.Initialize(*directory, *origin); e != nil {
			slog.Error("initialization failed")
			os.Exit(1)
		}
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if mode == "migrate" {
		// Migration credentials are never loaded from the runtime secret volume.
		if *config == "/run/secrets/config.json" {
			*config = "/run/migration/migration-config.json"
		}
		migration, err := planner.LoadMigrationConfig(*config)
		if err != nil {
			slog.Error("migration configuration unavailable")
			os.Exit(1)
		}
		store, err := planner.Open(ctx, migration.DatabaseURL)
		if err != nil {
			slog.Error("migration database unavailable")
			os.Exit(1)
		}
		defer store.DB.Close()
		if err = store.MigrateForRuntime(ctx, migration.RuntimeRole); err != nil {
			slog.Error("migration failed")
			os.Exit(1)
		}
		return
	}
	r, c, e := planner.LoadRuntime(*config)
	if e != nil {
		slog.Error("configuration unavailable")
		os.Exit(1)
	}
	s, e := planner.Open(ctx, r.DatabaseURL)
	if e != nil {
		slog.Error("database unavailable")
		os.Exit(1)
	}
	defer s.DB.Close()
	if e = s.CheckRuntimePrivileges(ctx); e != nil {
		slog.Error("runtime database role must be non-privileged; run database role setup")
		os.Exit(1)
	}

	a, e := planner.NewApp(s, c)
	if e != nil {
		slog.Error("invalid configuration")
		os.Exit(1)
	}
	switch mode {
	case "seed-calendar":
		var cal planner.Calendar
		b, err := os.ReadFile(*file)
		if err != nil {
			e = err
			break
		}
		if e = json.Unmarshal(b, &cal); e == nil {
			e = a.SeedCalendar(ctx, cal)
		}
	case "import":
		var p planner.Project
		b, err := os.ReadFile(*file)
		if err != nil {
			e = err
			break
		}
		if e = json.Unmarshal(b, &p); e == nil {
			e = a.Import(ctx, p, "", "local-migration")
		}
	case "recover-user":
		e = a.RecoverUser(ctx, *username, *file)
		if e == nil && *outputStdout {
			// Docker cannot copy files out of tmpfs on every host. This explicit local
			// transport is captured by the installer, never forwarded to its console.
			e = transferRecoveryFile(*file, os.Stdout)
		}
	case "worker":
		e = a.Worker(ctx)
	case "api":
		server := &http.Server{Addr: *addr, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
		go func() {
			<-ctx.Done()
			stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			server.Shutdown(stop)
		}()
		slog.Info("API starting", "version", planner.Version)
		e = server.ListenAndServe()
		if e == http.ErrServerClosed {
			e = nil
		}
	default:
		slog.Error("unknown command")
		os.Exit(2)
	}
	if e != nil {
		slog.Error("operation failed", "mode", mode, "reason", e.Error())
		os.Exit(1)
	}
}

// Called only after the recovery transaction commits, with explicit local opt-in.
func transferRecoveryFile(path string, output io.Writer) error {
	link, err := os.ReadFile(path)
	if err == nil {
		var n int
		n, err = output.Write(link)
		if err == nil && n != len(link) {
			err = io.ErrShortWrite
		}
	}
	if cleanupErr := os.Remove(path); err == nil {
		err = cleanupErr
	}
	return err
}
