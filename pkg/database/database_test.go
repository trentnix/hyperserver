package database

import (
	"errors"
	"testing"
)

func TestSetupRejectsUnregisteredDriver(t *testing.T) {
	db, err := Setup("hyperserver-unregistered-driver", "unused")
	if db != nil {
		db.Close()
		t.Fatal("unknown driver returned a pool")
	}
	if !errors.Is(err, ErrDatabaseConfiguration) {
		t.Fatalf("error = %v, want database configuration error", err)
	}
}
