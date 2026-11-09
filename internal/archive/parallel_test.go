// Copyright (C) 2026 Vinetwigs
// SPDX-License-Identifier: GPL-3.0-only

package archive

import (
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"testing"

	"3dfc/internal/ff"
)

func makeBig(t *testing.T, dir string, n int) []string {
	t.Helper()
	W, H := 96, 64
	var out []string
	for i := 0; i < n; i++ {
		img := image.NewNRGBA(image.Rect(0, 0, W, H))
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				img.SetNRGBA(x, y, color.NRGBA{uint8(x * 2), uint8(y * 3), 90, 255})
			}
		}
		for y := 20; y < 40; y++ {
			for x := (i * 7) % W; x < (i*7)%W+14 && x < W; x++ {
				img.SetNRGBA(x, y, color.NRGBA{10, 200, 60, 255})
			}
		}
		p := filepath.Join(dir, fmt.Sprintf("b_%04d.png", i))
		writePNG(t, p, img)
		out = append(out, p)
	}
	return out
}

func TestParallelGo(t *testing.T) {
	src := t.TempDir()
	orig := makeBig(t, src, 72)
	arch := filepath.Join(t.TempDir(), "p.3dfc")
	opt := DefaultOptions()
	opt.Backend = "go"
	opt.Preset = "fast"
	opt.GOPMax = 10
	if _, err := Create(src, arch, opt); err != nil {
		t.Fatal(err)
	}
	a, err := Open(arch)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(nil); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := a.ExtractRange(0, 71, out, nil); err != nil {
		t.Fatal(err)
	}
	checkEqual(t, src, out, orig)
	out = t.TempDir()
	if err := a.ExtractRange(5, 69, out, nil); err != nil {
		t.Fatal(err)
	}
	checkEqual(t, src, out, orig[5:70])
}

func TestParallelFFmpeg(t *testing.T) {
	if !ff.Available() {
		t.Skip("ffmpeg not in PATH")
	}
	src := t.TempDir()
	orig := makeBig(t, src, 72)
	arch := filepath.Join(t.TempDir(), "pf.3dfc")
	opt := DefaultOptions()
	opt.Backend = "ffmpeg"
	opt.Preset = "fast"
	opt.GOPMax = 10
	if _, err := Create(src, arch, opt); err != nil {
		t.Fatal(err)
	}
	a, err := Open(arch)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(nil); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := a.ExtractRange(0, 71, out, nil); err != nil {
		t.Fatal(err)
	}
	checkEqual(t, src, out, orig)
}
