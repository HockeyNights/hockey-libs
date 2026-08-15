package filestorage

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"

	"golang.org/x/image/draw"
)

const (
	JPEGContentType = "image/jpeg"

	defaultMaxSide = 512
	defaultQuality = 85
)

var (
	ErrTooLarge    = errors.New("filestorage: image is too large")
	ErrNotAnImage  = errors.New("filestorage: file is not a supported image")
	ErrTooManyPix  = errors.New("filestorage: image resolution is too high")
	maxPixelsLimit = 50_000_000
)

type ImageOptions struct {
	MaxBytes int64
	MaxSide  int
	Quality  int
}

func NormalizeImage(r io.Reader, opts ImageOptions) ([]byte, error) {
	if opts.MaxSide <= 0 {
		opts.MaxSide = defaultMaxSide
	}
	if opts.Quality <= 0 {
		opts.Quality = defaultQuality
	}

	var source io.Reader = r
	if opts.MaxBytes > 0 {
		source = io.LimitReader(r, opts.MaxBytes+1)
	}

	raw, err := io.ReadAll(source)
	if err != nil {
		return nil, fmt.Errorf("filestorage: read image: %w", err)
	}
	if opts.MaxBytes > 0 && int64(len(raw)) > opts.MaxBytes {
		return nil, ErrTooLarge
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrNotAnImage
	}
	if config.Width*config.Height > maxPixelsLimit {
		return nil, ErrTooManyPix
	}

	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrNotAnImage
	}

	out := fit(decoded, opts.MaxSide)

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, out, &jpeg.Options{Quality: opts.Quality}); err != nil {
		return nil, fmt.Errorf("filestorage: encode image: %w", err)
	}

	return encoded.Bytes(), nil
}

func fit(source image.Image, maxSide int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	if width <= maxSide && height <= maxSide {
		return source
	}

	if width > height {
		height = height * maxSide / width
		width = maxSide
	} else {
		width = width * maxSide / height
		height = maxSide
	}

	target := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(target, target.Bounds(), source, bounds, draw.Over, nil)

	return target
}
