// Command blog-maintenance creates and verifies archives and adopts blog schemas offline.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/internal/backup"
	"github.com/trentnix/hyperserver/modules/blog"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run loads live configuration for backup and adoption. Verification uses only the archive.
func run(ctx context.Context, args []string) error {
	const usage = "usage: blog-maintenance backup ARCHIVE | verify ARCHIVE | adopt-schema"
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch {
	case args[0] == "adopt-schema" && len(args) == 1:
		cfg, err := config.GetConfig()
		if err != nil {
			return err
		}
		if err := blog.AdoptSchema(ctx, cfg.App.WorkingDirectory); err != nil {
			return err
		}
		fmt.Println("Blog schema version 1 verified. Content and images are unchanged.")
	case args[0] == "backup" && len(args) == 2:
		cfg, err := config.GetConfig()
		if err != nil {
			return err
		}
		if err := backup.Create(ctx, cfg, args[1]); err != nil {
			return err
		}
		fmt.Println("Backup complete. Store it privately. Configuration files are unchanged. Save environment overrides separately.")
	case args[0] == "verify" && len(args) == 2:
		if err := backup.Verify(ctx, args[1]); err != nil {
			return err
		}
		fmt.Println("Backup verified. Checksums detect corruption, not a forged archive.")
	default:
		return errors.New(usage)
	}
	return nil
}
