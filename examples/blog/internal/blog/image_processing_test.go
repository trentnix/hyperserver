package blog

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// encodeTestImage produces real decoder inputs without external fixture files.
func encodeTestImage(t testing.TB, pixels image.Image, format string) []byte {
	t.Helper()
	var data bytes.Buffer
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(&data, pixels, &jpeg.Options{Quality: 98})
	} else {
		err = png.Encode(&data, pixels)
	}
	if err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

// TestImageCopies checks aspect ratio, no upscaling, transparency, and both formats.
func TestImageCopies(t *testing.T) {
	for _, tc := range []struct {
		name, format  string
		width, height int
	}{
		{"small", "png", 30, 20},
		{"wide", "png", 2600, 650},
		{"tall", "jpeg", 650, 2600},
		{"thin", "png", 2400, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pixels := image.NewNRGBA(image.Rect(0, 0, tc.width, tc.height))
			for y := range tc.height {
				for x := range tc.width {
					pixels.SetNRGBA(x, y, color.NRGBA{50, 100, 150, 128})
				}
			}
			data := encodeTestImage(t, pixels, tc.format)
			upload, err := prepareImage(t.Context(), data)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(upload.Original, data) || upload.MediaType != "image/"+tc.format {
				t.Fatal("original changed")
			}
			for _, part := range []struct {
				data  []byte
				limit int
			}{{upload.Display, displayImageSize}, {upload.Thumbnail, thumbnailImageSize}} {
				decoded, format, err := image.Decode(bytes.NewReader(part.data))
				if err != nil {
					t.Fatal(err)
				}
				width, height := tc.width, tc.height
				if longest := max(width, height); longest > part.limit {
					width, height = max(1, width*part.limit/longest), max(1, height*part.limit/longest)
				}
				if format != tc.format || decoded.Bounds() != image.Rect(0, 0, width, height) {
					t.Fatalf("wrong copy: %s %v", format, decoded.Bounds())
				}
				if format == "png" {
					_, _, _, alpha := decoded.At(0, 0).RGBA()
					if alpha != 128*257 {
						t.Fatalf("transparency changed: %d", alpha)
					}
				}
			}
		})
	}
}

// TestImageOrientation checks every EXIF transform, including reflected rotations.
func TestImageOrientation(t *testing.T) {
	pixels := image.NewGray(image.Rect(5, 7, 8, 9))
	copy(pixels.Pix, []byte{1, 2, 3, 4, 5, 6})
	for i, want := range [][]byte{{1, 2, 3, 4, 5, 6}, {3, 2, 1, 6, 5, 4}, {6, 5, 4, 3, 2, 1}, {4, 5, 6, 1, 2, 3}, {1, 4, 2, 5, 3, 6}, {4, 1, 5, 2, 6, 3}, {6, 3, 5, 2, 4, 1}, {3, 6, 2, 5, 1, 4}} {
		oriented := orientedImage(pixels, uint16(i+1))
		var got []byte
		b := oriented.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				got = append(got, color.GrayModel.Convert(oriented.At(x, y)).(color.Gray).Y)
			}
		}
		if !bytes.Equal(got, want) {
			t.Errorf("orientation %d: %v, want %v", i+1, got, want)
		}
	}
}

// jpegWithOrientation adds a minimal EXIF IFD and a private comment to a JPEG.
func jpegWithOrientation(data []byte, orientation uint16) []byte {
	exif := append([]byte{'E', 'x', 'i', 'f', 0, 0}, orientationMetadata(orientation)...)
	var result bytes.Buffer
	result.Write(data[:2])
	for _, part := range []struct {
		marker byte
		data   []byte
	}{{0xe1, exif}, {0xfe, []byte("private metadata")}} {
		result.Write([]byte{0xff, part.marker, byte((len(part.data) + 2) >> 8), byte(len(part.data) + 2)})
		result.Write(part.data)
	}
	result.Write(data[2:])
	return result.Bytes()
}

// orientationMetadata encodes a minimal TIFF IFD for JPEG and PNG EXIF fixtures.
func orientationMetadata(orientation uint16) []byte {
	data := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(data[18:20], orientation)
	return data
}

// TestImageEXIF verifies that decoding applies orientation and drops metadata.
func TestImageEXIF(t *testing.T) {
	data := encodeTestImage(t, image.NewRGBA(image.Rect(0, 0, 40, 20)), "jpeg")
	for orientation := uint16(1); orientation <= 8; orientation++ {
		upload, err := prepareImage(t.Context(), jpegWithOrientation(data, orientation))
		if err != nil {
			t.Fatal(err)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(upload.Display))
		width, height := 40, 20
		if orientation >= 5 {
			width, height = height, width
		}
		if err != nil || cfg.Width != width || cfg.Height != height {
			t.Fatalf("orientation %d: %+v %v", orientation, cfg, err)
		}
		for _, copy := range [][]byte{upload.Display, upload.Thumbnail} {
			if bytes.Contains(copy, []byte("Exif")) || bytes.Contains(copy, []byte("private metadata")) {
				t.Fatal("copy retained metadata")
			}
		}
	}
	if _, err := prepareImage(t.Context(), jpegWithOrientation(data, 9)); err == nil {
		t.Fatal("invalid orientation accepted")
	}
}

// pngWithChunk inserts a well-formed metadata or animation chunk after IHDR.
func pngWithChunk(data []byte, name string, payload []byte) []byte {
	chunk := make([]byte, 12+len(payload))
	binary.BigEndian.PutUint32(chunk, uint32(len(payload)))
	copy(chunk[4:], name)
	copy(chunk[8:], payload)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	return append(append(append([]byte{}, data[:33]...), chunk...), data[33:]...)
}

func TestImagePNGMetadataAndAnimation(t *testing.T) {
	data := testPNG(t)
	upload, err := prepareImage(t.Context(), pngWithChunk(data, "tEXt", []byte("Comment\x00private metadata")))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(upload.Display, []byte("private metadata")) {
		t.Fatal("PNG retained metadata")
	}
	animation := make([]byte, 8)
	binary.BigEndian.PutUint32(animation, 2)
	if _, err := prepareImage(t.Context(), pngWithChunk(data, "acTL", animation)); err == nil {
		t.Fatal("animated PNG flattened silently")
	}
	data = encodeTestImage(t, image.NewNRGBA(image.Rect(0, 0, 30, 20)), "png")
	upload, err = prepareImage(t.Context(), pngWithChunk(data, "eXIf", orientationMetadata(6)))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(upload.Display))
	if err != nil || cfg.Width != 20 || cfg.Height != 30 || bytes.Contains(upload.Display, []byte("eXIf")) {
		t.Fatalf("PNG orientation: %+v %v", cfg, err)
	}
}

// TestImageGIFPreserved checks frame timing and bytes survive without transcoding.
func TestImageGIFPreserved(t *testing.T) {
	palette := color.Palette{color.Transparent, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 3, 2), palette)
	second := image.NewPaletted(first.Bounds(), palette)
	second.SetColorIndex(0, 0, 1)
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{10, 20}, LoopCount: 2}); err != nil {
		t.Fatal(err)
	}
	upload, err := prepareImage(t.Context(), data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(upload.Original, data.Bytes()) || !bytes.Equal(upload.Display, data.Bytes()) || !bytes.Equal(upload.Thumbnail, data.Bytes()) {
		t.Fatal("GIF modified")
	}
}

func TestImageProcessingFailures(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("invalid"), testPNG(t)[:40], make([]byte, maxImageBytes+1)} {
		if _, err := prepareImage(t.Context(), data); err == nil {
			t.Fatal("invalid upload accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := prepareImage(ctx, testPNG(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := encodeImage(ctx, image.NewRGBA(image.Rect(0, 0, 1, 1)), "png", 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("encoder cancellation: %v", err)
	}
}

// BenchmarkPrepareImage measures a repeatable camera-sized JPEG workload.
func BenchmarkPrepareImage(b *testing.B) {
	pixels := image.NewRGBA(image.Rect(0, 0, 3000, 2000))
	for y := range 2000 {
		for x := range 3000 {
			pixels.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x + y), 255})
		}
	}
	data := encodeTestImage(b, pixels, "jpeg")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := prepareImage(b.Context(), data); err != nil {
			b.Fatal(err)
		}
	}
}
