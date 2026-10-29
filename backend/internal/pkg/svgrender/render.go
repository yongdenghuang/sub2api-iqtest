// Package svgrender renders untrusted SVG without host filesystem/network access.
package svgrender

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed resvg.wasm
var wasm []byte
var cache = wazero.NewCompilationCache()
var slot = make(chan struct{}, 1)

// PNG preserves the SVG viewport's aspect ratio within a 768-pixel longest edge.
func PNG(parent context.Context, svg string) ([]byte, int, int, error) {
	if len(svg) > 256<<10 {
		return nil, 0, 0, errors.New("SVG exceeds render limit")
	}
	w, h, err := dimensions(svg)
	if err != nil {
		return nil, 0, 0, err
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	case <-ctx.Done():
		return nil, 0, 0, ctx.Err()
	}
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCompilationCache(cache).WithMemoryLimitPages(4096).WithCloseOnContextDone(true))
	defer func() { _ = runtime.Close(context.Background()) }()
	if _, err = wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
		return nil, 0, 0, err
	}
	mod, err := runtime.InstantiateWithConfig(ctx, wasm, wazero.NewModuleConfig())
	if err != nil {
		return nil, 0, 0, err
	}
	alloc, render := mod.ExportedFunction("wasm_alloc"), mod.ExportedFunction("render")
	if alloc == nil || render == nil {
		return nil, 0, 0, errors.New("invalid renderer ABI")
	}
	ptr, err := alloc.Call(ctx, uint64(len(svg)))
	if err != nil {
		return nil, 0, 0, err
	}
	if len(ptr) != 1 || ptr[0] == 0 || !mod.Memory().Write(uint32(ptr[0]), []byte(svg)) {
		return nil, 0, 0, errors.New("renderer allocation failed")
	}
	result, err := render.Call(ctx, ptr[0], uint64(len(svg)), uint64(w), uint64(h))
	if err != nil {
		return nil, 0, 0, err
	}
	if len(result) != 1 {
		return nil, 0, 0, errors.New("renderer returned no image")
	}
	header, ok := mod.Memory().Read(uint32(result[0]), 20)
	if !ok || binary.LittleEndian.Uint32(header[:4]) != 0 {
		return nil, 0, 0, errors.New("SVG could not be rendered")
	}
	rw, rh := binary.LittleEndian.Uint32(header[4:8]), binary.LittleEndian.Uint32(header[8:12])
	data, lenData := binary.LittleEndian.Uint32(header[12:16]), binary.LittleEndian.Uint32(header[16:20])
	if rw != uint32(w) || rh != uint32(h) || lenData != uint32(w*h*4) {
		return nil, 0, 0, errors.New("unexpected rendered dimensions")
	}
	pixels, ok := mod.Memory().Read(data, lenData)
	if !ok {
		return nil, 0, 0, errors.New("renderer pixels out of bounds")
	}
	// tiny-skia returns premultiplied RGBA. Composite on white for vision input.
	source := &image.RGBA{Pix: pixels, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
	target := image.NewRGBA(source.Rect)
	draw.Draw(target, target.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(target, target.Bounds(), source, image.Point{}, draw.Over)
	var buf bytes.Buffer
	if err = png.Encode(&buf, target); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), w, h, nil
}

func dimensions(svg string) (int, int, error) {
	d := xml.NewDecoder(strings.NewReader(svg))
	for {
		token, err := d.Token()
		if err != nil {
			return 0, 0, err
		}
		root, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if root.Name.Local != "svg" {
			return 0, 0, errors.New("expected SVG root")
		}
		var w, h float64
		var viewbox string
		for _, a := range root.Attr {
			switch a.Name.Local {
			case "width":
				w, _ = strconv.ParseFloat(strings.TrimSuffix(a.Value, "px"), 64)
			case "height":
				h, _ = strconv.ParseFloat(strings.TrimSuffix(a.Value, "px"), 64)
			case "viewBox":
				viewbox = a.Value
			}
		}
		if w <= 0 || h <= 0 {
			parts := strings.Fields(strings.ReplaceAll(viewbox, ",", " "))
			if len(parts) == 4 {
				w, _ = strconv.ParseFloat(parts[2], 64)
				h, _ = strconv.ParseFloat(parts[3], 64)
			}
		}
		if w <= 0 || h <= 0 || math.IsInf(w, 0) || math.IsInf(h, 0) || math.IsNaN(w) || math.IsNaN(h) || w/h < 0.1 || w/h > 10 {
			return 0, 0, fmt.Errorf("unsupported SVG viewport")
		}
		scale := 768 / math.Max(w, h)
		return int(math.Round(w * scale)), int(math.Round(h * scale)), nil
	}
}
