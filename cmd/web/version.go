package main

import (
	"runtime/debug"
	"strings"
)

// Set by the linker when packaging a container. Never changed at runtime.
var version = "development"
var commit = "unknown"

// buildInfo uses linker metadata for packaged builds and VCS stamps for local builds.
func buildInfo() struct {
	Version, Commit string
	Modified        bool
} {
	result := struct {
		Version, Commit string
		Modified        bool
	}{Version: version, Commit: commit}
	result.Modified = strings.HasSuffix(commit, "-dirty")
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			result.Version = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				result.Commit = setting.Value
			case "vcs.modified":
				result.Modified = setting.Value == "true"
			}
		}
	}
	return result
}
