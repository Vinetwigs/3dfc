// Copyright (C) 2026 Vinetwigs
// SPDX-License-Identifier: GPL-3.0-only

package archive

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"3dfc/internal/ff"
)

func makeFrames(t *testing.T, dir string) []string {
	t.Helper()
	W, H, N := 128, 96, 16
	var names []string
	for i := 0; i < N; i++ {
		img := image.NewNRGBA(image.Rect(0, 0, W, H))
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				img.SetNRGBA(x, y, color.NRGBA{uint8(x * 2), uint8(y * 2), 100, 255})
			}
		}
		for y := 30; y < 50; y++ {
			for x := i * 6; x < i*6+16 && x < W; x++ {
				img.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
			}
		}
		if i == 6 {
			for y := 0; y < H; y++ {
				for x := 0; x < W; x++ {
					c := img.NRGBAAt(x, y)
					c.A = 128
					img.SetNRGBA(x, y, c)
				}
			}
		}
		p := filepath.Join(dir, fmt.Sprintf("f_%04d.png", i))
		if i == 11 {
			write16(t, p, W, H)
		} else {
			writePNG(t, p, img)
		}
		names = append(names, p)
	}
	return names
}

func writePNG(t *testing.T, p string, img image.Image) {
	t.Helper()
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func write16(t *testing.T, p string, W, H int) {
	t.Helper()
	img := image.NewRGBA64(image.Rect(0, 0, W, H))
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			img.SetRGBA64(x, y, color.RGBA64{uint16(x * 500), uint16(y * 500), 30000, 65535})
		}
	}
	writePNG(t, p, img)
}

func loadPix(t *testing.T, p string) []byte {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := img.(*image.NRGBA); ok {
		return append([]byte(nil), n.Pix...)
	}
	b := img.Bounds()
	r := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(r, r.Bounds(), img, b.Min, draw.Src)
	return r.Pix
}

func checkEqual(t *testing.T, origDir, outDir string, want []string) {
	t.Helper()
	for _, w := range want {
		name := filepath.Base(w)
		a := loadPix(t, w)
		b := loadPix(t, filepath.Join(outDir, name))
		if !bytes.Equal(a, b) {
			t.Fatalf("frame %s differs", name)
		}
	}
}

func roundTrip(t *testing.T, backend string) {
	t.Helper()
	src := t.TempDir()
	orig := makeFrames(t, src)
	arch := filepath.Join(t.TempDir(), "t.3dfc")
	opt := DefaultOptions()
	opt.Backend = backend
	opt.Preset = "fast"
	opt.GOPMax = 8
	rep, err := Create(src, arch, opt)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Frames != len(orig) || rep.Original <= 0 || rep.Size <= 0 {
		t.Fatalf("bad report: %+v", rep)
	}
	a, err := Open(arch)
	if err != nil {
		t.Fatal(err)
	}
	if a.Count() != len(orig) {
		t.Fatalf("count %d, want %d", a.Count(), len(orig))
	}
	if len(a.Summary()) == 0 {
		t.Fatal("empty summary")
	}
	if err := a.Verify(nil); err != nil {
		t.Fatal(err)
	}
	n := len(orig)
	out := t.TempDir()
	if err := a.ExtractRange(0, n-1, out, nil); err != nil {
		t.Fatal(err)
	}
	checkEqual(t, src, out, orig)
	out = t.TempDir()
	if err := a.ExtractRange(6, 6, out, nil); err != nil {
		t.Fatal(err)
	}
	checkEqual(t, src, out, orig[6:7])
	out = t.TempDir()
	if err := a.ExtractRange(0, 3, out, nil); err != nil {
		t.Fatal(err)
	}
	checkEqual(t, src, out, orig[:4])
	out = t.TempDir()
	if err := a.ExtractRange(9, 12, out, nil); err != nil {
		t.Fatal(err)
	}
	checkEqual(t, src, out, orig[9:13])
	if err := a.ExtractRange(0, n, out, nil); err == nil {
		t.Fatal("expected out-of-range error")
	}
	if err := a.ExtractRange(5, 2, out, nil); err == nil {
		t.Fatal("expected inverted-range error")
	}
}

func TestRoundTripGo(t *testing.T) { roundTrip(t, "go") }

func TestRoundTripFFmpeg(t *testing.T) {
	if !ff.Available() {
		t.Skip("ffmpeg not in PATH")
	}
	roundTrip(t, "ffmpeg")
}

func TestRoundTripNoise(t *testing.T) {
	src := t.TempDir()
	W, H, N := 160, 120, 12
	var orig []string
	for i := 0; i < N; i++ {
		img := image.NewNRGBA(image.Rect(0, 0, W, H))
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				v := uint8((x*73 + y*149 + i*211) % 256)
				img.SetNRGBA(x, y, color.NRGBA{v, uint8(255 - v), uint8(v / 2), 255})
			}
		}
		p := filepath.Join(src, fmt.Sprintf("n_%04d.png", i))
		writePNG(t, p, img)
		orig = append(orig, p)
	}
	arch := filepath.Join(t.TempDir(), "n.3dfc")
	opt := DefaultOptions()
	opt.Preset = "fast"
	opt.GOPMax = 8
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
	var sum int64
	for _, p := range orig {
		st, _ := os.Stat(p)
		sum += st.Size()
	}
	st, _ := os.Stat(arch)
	if st.Size() > sum+int64(1e6) {
		t.Fatalf("archive %d worse than originals %d", st.Size(), sum)
	}
	out := t.TempDir()
	if err := a.ExtractRange(0, N-1, out, nil); err != nil {
		t.Fatal(err)
	}
	checkEqual(t, src, out, orig)
}

func TestCreateRefusesOverwrite(t *testing.T) {
	src := t.TempDir()
	makeFrames(t, src)
	out := filepath.Join(t.TempDir(), "existing.3dfc")
	want := []byte("keep this file")
	if err := os.WriteFile(out, want, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(src, out, DefaultOptions()); err == nil {
		t.Fatal("expected existing archive to be protected")
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("existing archive contents changed")
	}
}

func TestCreateForceReplacesAtomically(t *testing.T) {
	src := t.TempDir()
	makeFrames(t, src)
	out := filepath.Join(t.TempDir(), "existing.3dfc")
	if err := os.WriteFile(out, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	opt := DefaultOptions()
	opt.Overwrite = true
	if _, err := Create(src, out, opt); err != nil {
		t.Fatal(err)
	}
	a, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	if a.Count() != 16 {
		t.Fatalf("archive has %d frames, want 16", a.Count())
	}
}

func TestCreateProtectsAndCanDiscardStalePartial(t *testing.T) {
	src := t.TempDir()
	makeFrames(t, src)
	out := filepath.Join(t.TempDir(), "frames.3dfc")
	partial := out + ".partial"
	if err := os.WriteFile(partial, []byte("interrupted"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(src, out, DefaultOptions()); err == nil {
		t.Fatal("expected stale partial protection")
	}
	opt := DefaultOptions()
	opt.Overwrite = true
	if _, err := Create(src, out, opt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatal("stale partial was not removed by explicit force")
	}
}

func TestExtractRefusesOverwrite(t *testing.T) {
	src := t.TempDir()
	makeFrames(t, src)
	archivePath := filepath.Join(t.TempDir(), "frames.3dfc")
	opt := DefaultOptions()
	opt.Backend = "go"
	opt.Preset = "fast"
	if _, err := Create(src, archivePath, opt); err != nil {
		t.Fatal(err)
	}
	a, err := Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	want := []byte("keep this frame")
	if err := os.WriteFile(filepath.Join(out, "f_0000.png"), want, 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.ExtractRange(0, 3, out, nil); err == nil {
		t.Fatal("expected existing frame to be protected")
	}
	got, err := os.ReadFile(filepath.Join(out, "f_0000.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("existing frame contents changed")
	}
}

func TestExtractOverwriteOption(t *testing.T) {
	src := t.TempDir()
	orig := makeFrames(t, src)
	archivePath := filepath.Join(t.TempDir(), "frames.3dfc")
	opt := DefaultOptions()
	opt.Backend = "go"
	opt.Preset = "fast"
	if _, err := Create(src, archivePath, opt); err != nil {
		t.Fatal(err)
	}
	a, err := Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, filepath.Base(orig[0])), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.ExtractRangeWithOptions(0, 3, out, ExtractOptions{Overwrite: true}, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := loadPix(t, filepath.Join(out, filepath.Base(orig[0]))), loadPix(t, orig[0]); !bytes.Equal(got, want) {
		t.Fatal("explicit overwrite did not restore original pixels")
	}
}

func TestWriteNamesRejectsLong(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "n.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := writeNames(f, []string{strings.Repeat("a", 70000)}); err == nil {
		t.Fatal("expected long-name error")
	}
}
