package util

import (
	"net/http"
	"strings"
)

// AddVary merges comma-separated fields without replacing existing Vary values.
// Field names are case-insensitive. An existing wildcard makes additions unnecessary.
func AddVary(header http.Header, values ...string) {
	seen := make(map[string]bool)
	for _, value := range header.Values("Vary") {
		for _, field := range strings.Split(value, ",") {
			seen[strings.ToLower(strings.TrimSpace(field))] = true
		}
	}
	for _, value := range values {
		for _, field := range strings.Split(value, ",") {
			field = strings.TrimSpace(field)
			key := strings.ToLower(field)
			if field != "" && !seen[key] && !seen["*"] {
				header.Add("Vary", field)
				seen[key] = true
			}
		}
	}
}
