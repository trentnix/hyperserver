package blog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
)

// TestRSSPostLimitSettings exercises defaults, strict YAML decoding, and startup validation.
func TestRSSPostLimitSettings(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        int
		invalid     bool
	}{
		{name: "omitted", want: 50},
		{name: "positive", input: "rssPostLimit: 7\n", want: 7},
		{name: "alias", input: "rssPostLimit: *count\n", want: 10},
		{name: "merged", input: "<<: {rssPostLimit: 7}\n", want: 7},
		{name: "unlimited", input: "rssPostLimit: 0\n"},
		{name: "negative", input: "rssPostLimit: -1\n", invalid: true},
		{name: "fraction", input: "rssPostLimit: 2.5\n", invalid: true},
		{name: "float", input: "rssPostLimit: 2.0\n", invalid: true},
		{name: "merged fraction", input: "<<: {rssPostLimit: 2.5}\n", invalid: true},
		{name: "exponent", input: "rssPostLimit: 2e1\n", invalid: true},
		{name: "text", input: "rssPostLimit: unlimited\n", invalid: true},
		{name: "quoted integer", input: "rssPostLimit: '5'\n", invalid: true},
		{name: "boolean", input: "rssPostLimit: true\n", invalid: true},
		{name: "null", input: "rssPostLimit: null\n", invalid: true},
		{name: "empty", input: "rssPostLimit:\n", invalid: true},
		{name: "list", input: "rssPostLimit: [5]\n", invalid: true},
		{name: "mapping", input: "rssPostLimit: {count: 5}\n", invalid: true},
		{name: "overflow", input: "rssPostLimit: 99999999999999999999\n", invalid: true},
		{name: "unknown field", input: "rssPostLimit: 5\nrssPostLimt: 5\n", invalid: true},
		{name: "unknown nested field", input: "storage: {unknown: value}\n", invalid: true},
		{name: "duplicate", input: "rssPostLimit: 5\nrssPostLimit: 6\n", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := &Site{}
			descriptor, err := site.Configure(assemblyApplication())
			if err != nil {
				t.Fatal(err)
			}
			m := descriptor.New().(*module)
			path := filepath.Join(t.TempDir(), "config.yaml")
			input := "adminEmail: admin@example.com\nrecentPostCount: &count 10\n" + tc.input
			if err := os.WriteFile(path, []byte(input), 0600); err != nil {
				t.Fatal(err)
			}
			_, err = config.LoadModuleFile(path, m.Configuration())
			if err == nil && m.settings.RSSPostLimit < 0 {
				// A negative limit must fail before accessing application resources.
				err = m.Init(t.Context(), nil)
				if err == nil || !strings.Contains(err.Error(), "blog.rssPostLimit") {
					t.Fatalf("negative RSS limit: %v", err)
				}
			}
			if (err != nil) != tc.invalid {
				t.Fatalf("error=%v, invalid=%v", err, tc.invalid)
			}
			if !tc.invalid && m.settings.RSSPostLimit != tc.want {
				t.Fatalf("rssPostLimit=%d, want %d", m.settings.RSSPostLimit, tc.want)
			}
		})
	}
}
