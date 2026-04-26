// kimo is a lightweight CTF instance manager with a pluggable provider
// architecture.
//
// Usage:
//
//	kimo serve -config kimo.yaml
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chxmxii/kimo/internal/api"
	"github.com/chxmxii/kimo/internal/config"
	"github.com/chxmxii/kimo/internal/instance"
	"github.com/chxmxii/kimo/internal/platform"
	"github.com/chxmxii/kimo/internal/pow"
	"github.com/chxmxii/kimo/internal/vm"

	// Register platform providers.
	_ "github.com/chxmxii/kimo/internal/platform/ctfd"

	// Register VM providers.
	_ "github.com/chxmxii/kimo/internal/vm/firecracker"
	_ "github.com/chxmxii/kimo/internal/vm/kubevirt"
)

func main() {
	logger := log.New(os.Stderr, "[kimo] ", log.LstdFlags)

	if len(os.Args) < 2 {
		usage(logger)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		if err := runServe(logger, os.Args[2:]); err != nil {
			logger.Fatalf("serve: %v", err)
		}
	case "providers":
		runProviders()
	case "version":
		fmt.Println("kimo v0.1.0")
	default:
		usage(logger)
		os.Exit(1)
	}
}

// runServe starts the HTTP API server.
func runServe(logger *log.Logger, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", "kimo.yaml", "path to configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Build platform provider.
	platformCfg, err := platformConfig(cfg)
	if err != nil {
		return err
	}
	platformProvider, err := platform.New(cfg.Platform.Type, platformCfg)
	if err != nil {
		return fmt.Errorf("creating platform provider: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := platformProvider.Validate(ctx); err != nil {
		return fmt.Errorf("platform validation: %w", err)
	}

	// Build VM provider.
	vmCfg, err := vmConfig(cfg)
	if err != nil {
		return err
	}
	vmProvider, err := vm.New(cfg.VM.Type, vmCfg)
	if err != nil {
		return fmt.Errorf("creating VM provider: %w", err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	if err := vmProvider.Validate(ctx2); err != nil {
		return fmt.Errorf("VM provider validation: %w", err)
	}

	// Build PoW manager.
	powMgr := pow.New(cfg.PoW.Difficulty, cfg.PoW.TTL)

	// Build instance manager.
	mgr := instance.New(platformProvider, vmProvider, powMgr)

	// Build and start HTTP server.
	srv := api.New(mgr, logger)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      srv,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Printf("listening on %s (platform=%s, vm=%s, pow.difficulty=%d)",
			addr, cfg.Platform.Type, cfg.VM.Type, cfg.PoW.Difficulty)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("HTTP server: %v", err)
		}
	}()

	<-done
	logger.Println("shutting down…")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	return httpSrv.Shutdown(shutCtx)
}

// runProviders prints registered providers.
func runProviders() {
	fmt.Println("Platform providers:", platform.Registered())
	fmt.Println("VM providers:", vm.Registered())
}

// platformConfig extracts the provider-specific config from the top-level Config.
func platformConfig(cfg *config.Config) (interface{}, error) {
	switch cfg.Platform.Type {
	case "ctfd":
		return cfg.Platform.CTFd, nil
	default:
		return nil, fmt.Errorf("unknown platform type %q", cfg.Platform.Type)
	}
}

// vmConfig extracts the provider-specific VM config.
func vmConfig(cfg *config.Config) (interface{}, error) {
	switch cfg.VM.Type {
	case "firecracker":
		return cfg.VM.Firecracker, nil
	case "kubevirt":
		return cfg.VM.KubeVirt, nil
	default:
		return nil, fmt.Errorf("unknown vm type %q", cfg.VM.Type)
	}
}

func usage(logger *log.Logger) {
	logger.Println("Usage: kimo <command> [flags]")
	logger.Println("Commands:")
	logger.Println("  serve     -config <path>   Start the HTTP API server")
	logger.Println("  providers                   List registered providers")
	logger.Println("  version                     Print version")
}
