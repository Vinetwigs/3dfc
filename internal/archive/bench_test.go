// Copyright (C) 2026 Vinetwigs
// SPDX-License-Identifier: GPL-3.0-only

package archive

import (
	"strings"
	"testing"

	"3dfc/internal/ff"
)

func TestBenchmarkGo(t *testing.T) {
	src := t.TempDir()
	makeFrames(t, src)
	rep, err := Benchmark(src, BenchOptions{
		Preset: "fast", GOPMax: 8, Tile: 64, FPS: 24,
		Backends: []string{"go"}, Verify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Frames != 16 || rep.Total != 16 || len(rep.Rows) != 1 {
		t.Fatalf("bad report: %+v", rep)
	}
	row := rep.Rows[0]
	if row.Backend != "go" || !row.Verified || row.Size <= 0 || row.Original <= 0 {
		t.Fatalf("bad row: %+v", row)
	}
	joined := strings.Join(rep.Lines(), "\n")
	for _, want := range []string{"benchmark:", "backend", "go", "saved"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("lines missing %q:\n%s", want, joined)
		}
	}
}

func TestBenchmarkMaxFrames(t *testing.T) {
	src := t.TempDir()
	makeFrames(t, src)
	rep, err := Benchmark(src, BenchOptions{
		Preset: "fast", GOPMax: 8, Backends: []string{"go"}, MaxFrames: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Frames != 5 || rep.Total != 16 {
		t.Fatalf("bad subset report: %+v", rep)
	}
}

func TestBenchmarkBadBackend(t *testing.T) {
	src := t.TempDir()
	makeFrames(t, src)
	if _, err := Benchmark(src, BenchOptions{Backends: []string{"nope"}}); err == nil {
		t.Fatal("expected backend error")
	}
}

func TestSplitBackends(t *testing.T) {
	want := 2
	if ff.Available() {
		want++
	}
	if got := SplitBackends("all"); len(got) != want {
		t.Fatalf("all = %v", got)
	}
	if got := SplitBackends("go, ffmpeg"); len(got) != 2 || got[0] != "go" || got[1] != "ffmpeg" {
		t.Fatalf("split = %v", got)
	}
}
