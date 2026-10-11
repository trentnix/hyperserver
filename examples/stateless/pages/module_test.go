package pages

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/trentnix/hyperserver/pkg/server"
	contentservice "github.com/trentnix/hyperserver/pkg/services/content"
)

func TestModuleRequiresRendering(t *testing.T) {
	for _, app := range []*server.ApplicationServer{nil, {}} {
		if err := Module().New().Init(context.Background(), app); err == nil {
			t.Fatal("initialization accepted missing rendering services")
		}
	}
}

func TestInitializationPreservesApplicationLayouts(t *testing.T) {
	cm := contentservice.NewContentManager()
	if err := ConfigureRendering(cm); err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"custom.html": {Data: []byte("custom")}}
	cm.RegisterLayouts(contentservice.PageType, files, "custom.html")
	cm.RegisterLayouts(contentservice.HtmxType, files, "custom.html")
	if err := Module().New().Init(t.Context(), &server.ApplicationServer{ContentManager: cm}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{contentservice.PageType, contentservice.HtmxType} {
		if layouts := cm.Layouts[kind]; len(layouts) != 1 || layouts[0].Path != "custom.html" {
			t.Fatalf("initialization changed %s layouts: %v", kind, layouts)
		}
	}
}
