// Package imaging prepares bounded JPEG, PNG, and GIF uploads without storage or HTTP dependencies.
package imaging

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/bep/imagemeta"
	"golang.org/x/image/draw"
)

// Options sets upload limits and output dimensions. Every field must be set.
type Options struct {
	// MaxBytes limits the encoded upload size.
	MaxBytes int
	// MaxPixels limits decoded width multiplied by height.
	MaxPixels int64
	// DisplaySize and ThumbnailSize limit each copy's longest edge.
	// ThumbnailSize must not exceed DisplaySize.
	DisplaySize, ThumbnailSize int
	// JPEGQuality must be from 1 to 100. PNG encoding is lossless.
	JPEGQuality int
}

func (o Options) validate() error {
	if o.MaxBytes <= 0 || o.MaxPixels <= 0 || o.DisplaySize <= 0 || o.ThumbnailSize <= 0 {
		return errors.New("image limits and dimensions must be positive")
	}
	if o.ThumbnailSize > o.DisplaySize {
		return errors.New("thumbnail size must not exceed display size")
	}
	if o.JPEGQuality < 1 || o.JPEGQuality > 100 {
		return errors.New("JPEG quality must be from 1 to 100")
	}
	return nil
}

// Result contains validated original bytes and ready-to-serve copies.
// Original aliases the input. Copies may share backing storage, especially for
// small images and GIFs. Treat all returned bytes as read-only.
type Result struct {
	MediaType                    string
	Original, Display, Thumbnail []byte
}

// Processing errors let callers choose their own response text.
var (
	ErrByteLimit    = errors.New("image exceeds encoded byte limit")
	ErrPixelLimit   = errors.New("image exceeds decoded pixel limit")
	ErrFormat       = errors.New("unsupported or unrecognized image format")
	ErrInvalidImage = errors.New("incomplete or damaged image")
	ErrOrientation  = errors.New("invalid image orientation metadata")
	ErrAnimation    = errors.New("animated PNG is not supported")
)

// Process validates an upload, corrects orientation, and creates JPEG/PNG copies
// without metadata. It never enlarges or crops images. GIF bytes, including all
// frames and metadata, remain unchanged after first-frame validation.
// Calls are independent and safe to run concurrently. Callers must bound concurrent
// work to control memory use. Cancellation is checked between I/O and processing
// stages, not during every decoder or resizer operation.
func Process(ctx context.Context, data []byte, options Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := options.validate(); err != nil {
		return Result{}, err
	}
	if len(data) > options.MaxBytes {
		return Result{}, ErrByteLimit
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "gif") {
		return Result{}, ErrFormat
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width) > options.MaxPixels/int64(cfg.Height) {
		return Result{}, ErrPixelLimit
	}
	pixels, _, err := image.Decode(contextReader{ctx, bytes.NewReader(data)})
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, ErrInvalidImage
	}
	upload := Result{MediaType: "image/" + format, Original: data}
	if format == "gif" {
		upload.Display, upload.Thumbnail = data, data
		return upload, ctx.Err()
	}

	orientation := uint16(1)
	metadataFormat := imagemeta.JPEG
	if format == "png" {
		metadataFormat = imagemeta.PNG
	}
	metadataWarning := false
	metadata, err := imagemeta.Decode(imagemeta.Options{
		R: bytes.NewReader(data), ImageFormat: metadataFormat, Sources: imagemeta.EXIF | imagemeta.CONFIG,
		ShouldHandleTag: func(tag imagemeta.TagInfo) bool { return tag.Namespace == "IFD0" && tag.Tag == "Orientation" },
		HandleTag: func(tag imagemeta.TagInfo) error {
			value, ok := tag.Value.(uint16)
			if !ok || value < 1 || value > 8 {
				return errors.New("invalid orientation")
			}
			orientation = value
			return nil
		},
		Warnf: func(string, ...any) { metadataWarning = true },
	})
	if err != nil || metadataWarning {
		return Result{}, ErrOrientation
	}
	if metadata.ImageConfig.FrameCount > 1 {
		return Result{}, ErrAnimation
	}
	pixels = orientedImage(pixels, orientation)
	if upload.Display, err = encodeImage(ctx, pixels, format, options.DisplaySize, options.JPEGQuality); err != nil {
		return Result{}, err
	}
	if max(pixels.Bounds().Dx(), pixels.Bounds().Dy()) <= options.ThumbnailSize {
		upload.Thumbnail = upload.Display
	} else if upload.Thumbnail, err = encodeImage(ctx, pixels, format, options.ThumbnailSize, options.JPEGQuality); err != nil {
		return Result{}, err
	}
	return upload, ctx.Err()
}

// orientedPixels exposes corrected coordinates without allocating another full-sized image.
type orientedPixels struct {
	image.Image
	orientation uint16
}

// Bounds reports the visible dimensions after any quarter turn.
func (p orientedPixels) Bounds() image.Rectangle {
	b := p.Image.Bounds()
	if p.orientation >= 5 {
		return image.Rect(0, 0, b.Dy(), b.Dx())
	}
	return image.Rect(0, 0, b.Dx(), b.Dy())
}

// At maps display coordinates to the original pixels for all eight EXIF orientations.
func (p orientedPixels) At(x, y int) color.Color {
	return p.RGBA64At(x, y)
}

// RGBA64At lets the resizer read oriented pixels without allocating a color per pixel.
func (p orientedPixels) RGBA64At(x, y int) color.RGBA64 {
	b := p.Image.Bounds()
	w, h := b.Dx(), b.Dy()
	switch p.orientation {
	case 2:
		x = w - 1 - x
	case 3:
		x, y = w-1-x, h-1-y
	case 4:
		y = h - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, h-1-x
	case 7:
		x, y = w-1-y, h-1-x
	case 8:
		x, y = w-1-y, x
	}
	if source, ok := p.Image.(image.RGBA64Image); ok {
		return source.RGBA64At(x+b.Min.X, y+b.Min.Y)
	}
	return color.RGBA64Model.Convert(p.Image.At(x+b.Min.X, y+b.Min.Y)).(color.RGBA64)
}

// orientedImage retains decoder fast paths when no orientation correction is needed.
func orientedImage(pixels image.Image, orientation uint16) image.Image {
	if orientation == 1 {
		return pixels
	}
	return orientedPixels{pixels, orientation}
}

// encodeImage bounds the longest edge without enlarging or cropping. Re-encoding
// uses only pixel data, so originals' EXIF, GPS, and text metadata are not copied.
func encodeImage(ctx context.Context, pixels image.Image, format string, limit, quality int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b := pixels.Bounds()
	if longest := max(b.Dx(), b.Dy()); longest > limit {
		width, height := max(1, b.Dx()*limit/longest), max(1, b.Dy()*limit/longest)
		resized := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(resized, resized.Bounds(), pixels, b, draw.Src, nil)
		pixels = resized
	}
	var output bytes.Buffer
	writer := contextWriter{ctx, &output}
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(writer, pixels, &jpeg.Options{Quality: quality})
	} else {
		err = png.Encode(writer, pixels)
	}
	if err != nil {
		return nil, err
	}
	return output.Bytes(), ctx.Err()
}

// contextReader lets decoders notice cancellation between reads.
type contextReader struct {
	ctx context.Context
	io.Reader
}

// Read stops a decoder when the request is canceled.
func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// contextWriter lets encoders and storage writes stop between chunks.
type contextWriter struct {
	ctx context.Context
	io.Writer
}

// Write stops an encoder or staged upload when the request is canceled.
func (w contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.Writer.Write(p)
}
