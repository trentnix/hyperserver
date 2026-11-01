package blog

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"
)

// TestPrepareImage keeps the blog's processing policy and editor feedback explicit.
func TestPrepareImage(t *testing.T) {
	for _, tc := range []struct {
		data []byte
		want string
	}{
		{nil, "Choose a valid PNG, JPEG, or GIF."},
		{make([]byte, maxImageBytes+1), "Choose an image of at most 8 MiB."},
		{testPNG(t)[:40], "The image is incomplete or damaged."},
	} {
		if _, err := prepareImage(t.Context(), tc.data); err == nil || err.Error() != tc.want {
			t.Fatalf("processing error = %v, want %q", err, tc.want)
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2400, 600))); err != nil {
		t.Fatal(err)
	}
	upload, err := prepareImage(t.Context(), data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(upload.Original, data.Bytes()) || upload.MediaType != "image/png" {
		t.Fatal("original or media type changed")
	}
	for _, tc := range []struct {
		data          []byte
		width, height int
	}{{upload.Display, 2048, 512}, {upload.Thumbnail, 480, 120}} {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(tc.data))
		if err != nil || cfg.Width != tc.width || cfg.Height != tc.height {
			t.Fatalf("copy dimensions: %+v, %v", cfg, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := prepareImage(ctx, testPNG(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}
