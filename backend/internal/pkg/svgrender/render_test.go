package svgrender

import (
	"bytes"
	"context"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPNGRendersActualPixelsAndPreservesAspect(t *testing.T) {
	data, w, h, err := PNG(context.Background(), `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100"><rect x="0" y="0" width="100" height="100" fill="red"/></svg>`)
	require.NoError(t, err)
	require.Equal(t, 768, w)
	require.Equal(t, 384, h)
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	r, g, b, _ := img.At(100, 100).RGBA()
	require.Greater(t, r, uint32(60000))
	require.Zero(t, g)
	require.Zero(t, b)
	r, g, b, _ = img.At(650, 100).RGBA()
	require.Equal(t, uint32(65535), r)
	require.Equal(t, r, g)
	require.Equal(t, r, b)
}
func TestPNGBoundsAndCancellation(t *testing.T) {
	for _, svg := range []string{`<svg viewBox="0 0 1000000000 1"/>`, `<svg width="NaN" height="10"/>`, `<html/>`} {
		_, _, _, err := PNG(context.Background(), svg)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err := PNG(ctx, `<svg viewBox="0 0 100 100"/>`)
	require.Error(t, err)
}
