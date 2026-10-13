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
	var configurationError *ErrDatabaseConfiguration
	if !errors.As(err, &configurationError) {
		t.Fatalf("error = %v, want database configuration error", err)
	}
}
