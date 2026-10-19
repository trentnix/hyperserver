// Package setup selects storage providers for the reference maintenance command.
package setup

import (
	"context"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/configured"
)

// CleanupAccountTokens removes one bounded batch without initializing account
// schema. Providers close acquired resources. The caller owns the deadline.
func CleanupAccountTokens(ctx context.Context, cfg *config.Config, limit int) (int64, error) {
	return configured.CleanupAccountTokens(ctx, cfg, limit, nil)
}
