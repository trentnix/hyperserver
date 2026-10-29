package imaging_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"

	"github.com/trentnix/hyperserver/pkg/imaging"
)

func ExampleProcess() {
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewRGBA(image.Rect(0, 0, 100, 50))); err != nil {
		panic(err)
	}
	result, err := imaging.Process(context.Background(), input.Bytes(), imaging.Options{
		MaxBytes: 8 * 1024 * 1024, MaxPixels: 20_000_000,
		DisplaySize: 2048, ThumbnailSize: 480, JPEGQuality: 85,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	// The caller chooses storage, access rules, and how to serve these copies.
	fmt.Println(result.MediaType, len(result.Original) > 0, len(result.Thumbnail) > 0)
	// Output: image/png true true
}
