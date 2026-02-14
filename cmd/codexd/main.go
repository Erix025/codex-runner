package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	"codex-runner/internal/codexd/config"
	"codex-runner/internal/codexd/service"
	"codex-runner/internal/shared/selfupdate"
)

const defaultCodexdConfigPath = "~/.config/codexd/config.yaml"

func main() {
	log.SetFlags(0)

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	case "version":
		fmt.Println(service.Version)
	case "update":
		update(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "codexd: remote exec daemon")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  codexd serve [--config <path>]")
	fmt.Fprintln(os.Stderr, "  codexd version")
	fmt.Fprintln(os.Stderr, "  codexd update [--check] [--yes]")
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", defaultCodexdConfigPath, "path to config yaml")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	created, p, err := config.EnsureDefaultConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to bootstrap config:", err)
		os.Exit(2)
	}
	if created {
		fmt.Fprintln(os.Stderr, "created default config:", p)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load config:", err)
		os.Exit(2)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "failed to create data_dir:", err)
		os.Exit(2)
	}

	svc := service.New(cfg)
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           svc.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Fprintln(os.Stderr, "listening on", cfg.Listen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}

func update(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	checkOnly := fs.Bool("check", false, "check latest release only")
	yes := fs.Bool("yes", false, "apply update without prompt")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	u := selfupdate.Updater{
		BinaryName:     "codexd",
		CurrentVersion: service.Version,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	check, err := u.Check(ctx, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		fmt.Fprintln(os.Stderr, "update check failed:", err)
		os.Exit(1)
	}
	if *checkOnly {
		fmt.Fprintf(
			os.Stdout,
			"{\"binary\":\"codexd\",\"current_version\":%q,\"latest_version\":%q,\"comparable\":%t,\"update_available\":%t,\"asset\":%q}\n",
			check.CurrentVersion,
			check.LatestVersion,
			check.Comparable,
			check.UpdateAvailable,
			check.AssetName,
		)
		return
	}
	if check.Comparable && !check.UpdateAvailable {
		fmt.Fprintf(os.Stdout, "codexd is up to date (%s)\n", check.CurrentVersion)
		return
	}
	if !*yes {
		fmt.Fprintf(os.Stderr, "update codexd from %s to %s? use --yes to confirm\n", check.CurrentVersion, check.LatestVersion)
		os.Exit(2)
	}
	latest, err := u.Update(ctx, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		fmt.Fprintln(os.Stderr, "update failed:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "updated codexd to %s\n", latest)
}
