package module_site

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContactRejectsMalformedForm(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader("message=private%ZZ"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	// No services: a parse failure must return before rendering or persistence.
	new(SiteModule).Contact(w, r)
	if w.Code != http.StatusBadRequest || w.Body.String() != "Unable to parse form data\n" {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}
