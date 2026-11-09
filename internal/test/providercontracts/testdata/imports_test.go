package provider_test

import (
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/sessiontest"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/services/user/accounttest"
)

// Check every suite entry point from outside HyperServer's module namespace.
var (
	_ func(*testing.T, func(*testing.T) user.AccountRepository)            = accounttest.Run
	_ func(*testing.T, func(*testing.T) user.AccountRepository, ...string) = accounttest.RunTokens
	_ func(*testing.T, func(*testing.T) session.SessionStore)              = sessiontest.Run
	_ func(*testing.T, func(*testing.T) session.SessionStore)              = sessiontest.RunCommon
)
