package auth

import (
	"testing"

	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestModuleSuppliesServicesWithoutChangingCatalog(t *testing.T) {
	accounts := &struct{ user.AccountRepository }{}
	sessions := new(session.SessionManager)
	descriptor := Module(accounts, sessions, nil)
	first, second := descriptor.New().(*module), descriptor.New().(*module)
	if first == second || first.manager == second.manager {
		t.Fatal("factory reused module or manager state")
	}
	if first.manager.accounts != accounts || second.manager.accounts != accounts || first.manager.sessions != sessions || second.manager.sessions != sessions {
		t.Fatal("factory did not supply the configured services")
	}
	otherAccounts := &struct{ user.AccountRepository }{}
	otherSessions := new(session.SessionManager)
	other := Module(otherAccounts, otherSessions, nil).New().(*module)
	if other.manager.accounts != otherAccounts || first.manager.accounts != accounts || other.manager.sessions != otherSessions || first.manager.sessions != sessions {
		t.Fatal("one application's selection changed another application's services")
	}

	for _, registered := range handlers.Registered() {
		if registered.Name == descriptor.Name {
			manager := registered.New().(*module).manager
			if manager.accounts != nil || manager.sessions != nil {
				t.Fatal("application services escaped into the import catalog")
			}
			return
		}
	}
	t.Fatal("authentication did not self-register")
}
