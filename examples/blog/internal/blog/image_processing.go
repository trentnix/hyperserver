package blog

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

const displayImageSize = 2048
const thumbnailImageSize = 480
const imageJPEGQuality = 85

// imageUpload is one validated original and its ready-to-serve copies.
// GIFs retain their bytes, including animation and metadata, in every representation.
type imageUpload struct {
	MediaType                    string
	Original, Display, Thumbnail []byte
}

// prepareImage validates limits before decoding, applies orientation, and encodes
// metadata-free JPEG/PNG copies. It does not read storage or decide visibility.
func prepareImage(ctx context.Context, data []byte) (imageUpload, error) {
	if err := ctx.Err(); err != nil {
		return imageUpload{}, err
	}
	if len(data) > maxImageBytes {
		return imageUpload{}, errors.New("Choose an image of at most 8 MiB.")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "gif") {
		return imageUpload{}, errors.New("Choose a valid PNG, JPEG, or GIF.")
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 20_000_000 {
		return imageUpload{}, errors.New("Images must contain at most 20 million pixels.")
	}
	pixels, _, err := image.Decode(contextReader{ctx, bytes.NewReader(data)})
	if err != nil {
		if ctx.Err() != nil {
			return imageUpload{}, ctx.Err()
		}
		return imageUpload{}, errors.New("The image is incomplete or damaged.")
	}
	upload := imageUpload{MediaType: "image/" + format, Original: data}
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
		return imageUpload{}, errors.New("Unable to read image orientation. Export the image without metadata and try again.")
	}
	if metadata.ImageConfig.FrameCount > 1 {
		return imageUpload{}, errors.New("Animated PNGs are not supported. Use an animated GIF instead.")
	}
	pixels = orientedImage(pixels, orientation)
	if upload.Display, err = encodeImage(ctx, pixels, format, displayImageSize); err != nil {
		return imageUpload{}, err
	}
	if max(pixels.Bounds().Dx(), pixels.Bounds().Dy()) <= thumbnailImageSize {
		upload.Thumbnail = upload.Display
	} else if upload.Thumbnail, err = encodeImage(ctx, pixels, format, thumbnailImageSize); err != nil {
		return imageUpload{}, err
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
func encodeImage(ctx context.Context, pixels image.Image, format string, limit int) ([]byte, error) {
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
		err = jpeg.Encode(writer, pixels, &jpeg.Options{Quality: imageJPEGQuality})
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
