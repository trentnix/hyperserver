package deployment

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestRehearsalPinsCommandTargets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COMPOSE_FILE", "/wrong/compose.yaml")
	t.Setenv("COMPOSE_PROJECT_NAME", "production")
	t.Setenv("COMPOSE_PROFILES", "all")
	cmd := localCommand(t.Context(), dir, "docker", "compose", "stop", "app")
	want := []string{"docker", "--context", "default", "compose", "--file", filepath.Join(dir, "compose.yaml"), "--env-file", filepath.Join(dir, ".env"), "--project-name", "hyperserver-rehearsal", "stop", "app"}
	if !slices.Equal(cmd.Args, want) || cmd.Dir != dir {
		t.Fatal(cmd.Args, cmd.Dir)
	}
	if cmd.Env[len(cmd.Env)-1] != "COMPOSE_PROFILES=" {
		t.Fatal("inherited active profiles")
	}
	cmd = localCommand(t.Context(), dir, "docker", "inspect", "fixture")
	if !slices.Equal(cmd.Args, []string{"docker", "--context", "default", "inspect", "fixture"}) {
		t.Fatal(cmd.Args)
	}
}
