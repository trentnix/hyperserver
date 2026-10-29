// Command optimize-images regenerates blog image copies while the server is stopped.
// Run it from the blog application directory, using its existing configuration files.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/examples/blog/internal/blog"
)

// main loads only configuration and image storage, not HTTP or account services.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run reports partial progress so an interrupted batch can be safely rerun.
func run(ctx context.Context) error {
	cfg, err := config.GetConfig()
	if err != nil {
		return err
	}
	var settings blog.Settings
	path := filepath.Join(cfg.App.WorkingDirectory, "modules", "blog", "config.yaml")
	found, err := config.LoadModuleFile(path, &settings)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("module configuration not found: %s", path)
	}
	count, err := blog.OptimizeImages(ctx, settings.UploadsDirectory, cfg.App.WorkingDirectory)
	fmt.Printf("Optimized %d images.\n", count)
	return err
}
