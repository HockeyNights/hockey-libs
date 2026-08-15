package filestorage_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/HockeyNights/hockey-libs/filestorage"
	"github.com/stretchr/testify/require"
)

func sample(t *testing.T, width, height int, encode func(*bytes.Buffer, image.Image) error) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := range width {
		for y := range height {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 100, A: 255})
		}
	}

	var buf bytes.Buffer
	require.NoError(t, encode(&buf, img))

	return buf.Bytes()
}

func asJPEG(buf *bytes.Buffer, img image.Image) error {
	return jpeg.Encode(buf, img, nil)
}

func asPNG(buf *bytes.Buffer, img image.Image) error {
	return png.Encode(buf, img)
}

func TestNormalizeShrinksLargeImage(t *testing.T) {
	source := sample(t, 1600, 900, asJPEG)

	out, err := filestorage.NormalizeImage(bytes.NewReader(source), filestorage.ImageOptions{MaxSide: 512})
	require.NoError(t, err)

	config, format, err := image.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.Equal(t, 512, config.Width)
	require.Equal(t, 288, config.Height)
}

func TestNormalizeKeepsSmallImage(t *testing.T) {
	source := sample(t, 100, 80, asJPEG)

	out, err := filestorage.NormalizeImage(bytes.NewReader(source), filestorage.ImageOptions{MaxSide: 512})
	require.NoError(t, err)

	config, _, err := image.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	require.Equal(t, 100, config.Width)
}

func TestNormalizeConvertsPNGToJPEG(t *testing.T) {
	source := sample(t, 200, 200, asPNG)

	out, err := filestorage.NormalizeImage(bytes.NewReader(source), filestorage.ImageOptions{})
	require.NoError(t, err)

	_, format, err := image.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
}

func TestNormalizeRejectsNonImage(t *testing.T) {
	_, err := filestorage.NormalizeImage(strings.NewReader("<?php system($_GET[0]); ?>"), filestorage.ImageOptions{})

	require.ErrorIs(t, err, filestorage.ErrNotAnImage)
}

func TestNormalizeRejectsOversized(t *testing.T) {
	source := sample(t, 800, 800, asJPEG)

	_, err := filestorage.NormalizeImage(bytes.NewReader(source), filestorage.ImageOptions{MaxBytes: 100})

	require.ErrorIs(t, err, filestorage.ErrTooLarge)
}

func TestNormalizeStripsTrailingPayload(t *testing.T) {
	source := append(sample(t, 64, 64, asJPEG), []byte("<?php evil(); ?>")...)

	out, err := filestorage.NormalizeImage(bytes.NewReader(source), filestorage.ImageOptions{})
	require.NoError(t, err)
	require.NotContains(t, string(out), "php")
}
