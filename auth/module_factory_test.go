package auth

import (
	"testing"

	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestModuleSuppliesAccountsWithoutChangingCatalog(t *testing.T) {
	accounts := &struct{ user.AccountRepository }{}
	descriptor := Module(accounts, nil)
	first, second := descriptor.New().(*module), descriptor.New().(*module)
	if first == second || first.manager == second.manager {
		t.Fatal("factory reused module or manager state")
	}
	if first.manager.accounts != accounts || second.manager.accounts != accounts {
		t.Fatal("factory did not supply the configured repository")
	}
	otherAccounts := &struct{ user.AccountRepository }{}
	other := Module(otherAccounts, nil).New().(*module)
	if other.manager.accounts != otherAccounts || first.manager.accounts != accounts {
		t.Fatal("one application's selection changed another application's repository")
	}

	for _, registered := range handlers.Registered() {
		if registered.Name == descriptor.Name {
			if registered.New().(*module).manager.accounts != nil {
				t.Fatal("application accounts escaped into the import catalog")
			}
			return
		}
	}
	t.Fatal("authentication did not self-register")
}
