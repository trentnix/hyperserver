package util

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestLoadHTMLFromExplicitFilesystem(t *testing.T) {
	files := fstest.MapFS{"button.html": {Data: []byte("<button>Log in</button>")}}
	got, err := LoadHTML(files, "button.html")
	if err != nil || string(got) != "<button>Log in</button>" {
		t.Fatalf("trusted markup=%q error=%v", got, err)
	}
	if _, err := LoadHTML(files, "missing.html"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("missing file error:", err)
	}
	if _, err := LoadHTML(nil, "button.html"); err == nil {
		t.Fatal("nil filesystem accepted")
	}
}
