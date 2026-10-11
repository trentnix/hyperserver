package pages

import (
	"context"
	"testing"

	"github.com/trentnix/hyperserver/pkg/server"
)

func TestModuleRequiresRendering(t *testing.T) {
	for _, app := range []*server.ApplicationServer{nil, {}} {
		if err := Module().New().Init(context.Background(), app); err == nil {
			t.Fatal("initialization accepted missing rendering services")
		}
	}
}
