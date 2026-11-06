package blog

import (
	"os"
	"testing"
)

func TestBrowserUsers(t *testing.T) {
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Skip("set HS_TEST_FIREFOX=firefox to check user management")
	}
	for _, width := range []string{"320", "1100", "3440"} {
		t.Run(width, func(t *testing.T) { testBrowser(t, browser, width, "users") })
	}
}
