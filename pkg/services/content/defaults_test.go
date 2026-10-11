package content

import (
	"strings"
	"testing"
)

func TestRenderingDefaultsHaveOneOwner(t *testing.T) {
	for _, second := range []string{"site", "other"} {
		t.Run(second, func(t *testing.T) {
			cm := NewContentManager()
			if err := cm.ApplyDefaults("site", func(cm *ContentManagerService) { cm.AuthURL = "/login" }); err != nil {
				t.Fatal(err)
			}
			cm.AuthURL = "/custom-login"
			called := false
			err := cm.ApplyDefaults(second, func(*ContentManagerService) { called = true })
			if err == nil || !strings.Contains(err.Error(), `already owned by "site"`) || called || cm.AuthURL != "/custom-login" {
				t.Fatalf("conflicting policy changed overrides: error=%v called=%t url=%q", err, called, cm.AuthURL)
			}
		})
	}
}

func TestRenderingDefaultsRejectInvalidSetup(t *testing.T) {
	for _, owner := range []string{"", " "} {
		cm := NewContentManager()
		if err := cm.ApplyDefaults(owner, func(*ContentManagerService) { t.Fatal("invalid setup called configure") }); err == nil {
			t.Fatal("accepted empty owner")
		}
		if err := cm.ApplyDefaults("valid", func(*ContentManagerService) {}); err != nil {
			t.Fatal("invalid setup claimed ownership:", err)
		}
	}
	cm := NewContentManager()
	if err := cm.ApplyDefaults("site", nil); err == nil {
		t.Fatal("accepted nil configure")
	}
	if err := cm.ApplyDefaults("site", func(*ContentManagerService) {}); err != nil {
		t.Fatal(err)
	}
	if err := (*ContentManagerService)(nil).ApplyDefaults("site", func(*ContentManagerService) {}); err == nil {
		t.Fatal("accepted nil content manager")
	}
}
