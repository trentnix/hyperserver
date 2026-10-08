package util

import (
	"net/http"
	"reflect"
	"testing"
)

func TestAddVary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing []string
		add      []string
		want     []string
	}{
		{"empty", nil, []string{"HX-Request"}, []string{"HX-Request"}},
		{"preserve lines", []string{"Accept-Encoding, Origin", "Accept-Language"}, []string{"HX-Request"}, []string{"Accept-Encoding, Origin", "Accept-Language", "HX-Request"}},
		{"case and whitespace", []string{"Origin, hx-request "}, []string{"HX-Request", " origin "}, []string{"Origin, hx-request "}},
		{"merge lists", []string{"Origin"}, []string{"Accept-Encoding, HX-Request", "hx-request, ,"}, []string{"Origin", "Accept-Encoding", "HX-Request"}},
		{"wildcard", []string{"*"}, []string{"HX-Request"}, []string{"*"}},
		{"wildcard on another line", []string{"Origin", " * "}, []string{"HX-Request"}, []string{"Origin", " * "}},
		{"add wildcard", []string{"Origin"}, []string{"*, HX-Request"}, []string{"Origin", "*"}},
		{"empty additions", nil, []string{" , "}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := make(http.Header)
			for _, value := range tc.existing {
				header.Add("Vary", value)
			}
			AddVary(header, tc.add...)
			AddVary(header, tc.add...) // Repeated application must not duplicate fields.
			if got := header.Values("Vary"); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Vary = %q, want %q", got, tc.want)
			}
		})
	}
}
