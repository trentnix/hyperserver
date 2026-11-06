package sqlite

import "testing"

func TestAccessMigrationPreservesAccounts(t *testing.T) {
	db := unpreparedUserTestDB(t)
	if _, err := db.Exec(accountBaselineSQL(t) + accountBaselineRows); err != nil {
		t.Fatal(err)
	}
	before := accountBaselineDataSnapshot(t, db)
	up, err := accountMigrations.ReadFile("migrations/000002_access.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := accountMigrations.ReadFile("migrations/000002_access.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := db.Exec(string(up)); err != nil {
			t.Fatal(err)
		}
		var role string
		var invited bool
		if err := db.QueryRow("SELECT role,invited FROM user WHERE id='account'").Scan(&role, &invited); err != nil || role != "" || invited {
			t.Fatalf("upgrade defaults: %q %t %v", role, invited, err)
		}
		if _, err := db.Exec(`UPDATE user SET role='poster',invited=1;
INSERT INTO usertoken(user_id,token_hash,expires_at,token_type,email) VALUES('account','invitation','2030-02-01','auth-invitation','person@example.invalid');`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(down)); err != nil {
			t.Fatal(err)
		}
		if got := accountBaselineDataSnapshot(t, db); got != before {
			t.Fatal("downgrade changed credentials, identities, or unrelated tokens")
		}
	}
}
