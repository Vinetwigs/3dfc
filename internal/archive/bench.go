package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"3dfc/internal/ff"
)

type BenchOptions struct {
	Preset    string
	GOPMax    int
	Tile      int
	FPS       int
	Backends  []string
	MaxFrames int
	Verify    bool
	Keep      bool
	Progress  func(backend, stage string, done, total int)
}

type BenchRow struct {
	Backend  string
	Frames   int
	Original int64
	Size     int64
	Encode   time.Duration
	Verified bool
	Path     string
}

type BenchReport struct {
	Frames   int
	Total    int
	Original int64
	Rows     []BenchRow
}

func (r *BenchRow) Saved() int64 {
	return r.Original - r.Size
}

func (r *BenchRow) SavedPct() float64 {
	if r.Original <= 0 {
		return 0
	}
	return 100 * float64(r.Saved()) / float64(r.Original)
}

func (r *BenchRow) Speed() float64 {
	s := r.Encode.Seconds()
	if s <= 0 || r.Original <= 0 {
		return 0
	}
	return float64(r.Original) / 1e6 / s
}

func benchDur(d time.Duration) string {
	s := int(d.Seconds())
	if s >= 60 {
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%ds", s)
}

func (r *BenchReport) Lines() []string {
	head := fmt.Sprintf("benchmark: %d frames · original %s", r.Frames, FormatSize(r.Original))
	if r.Total > r.Frames {
		head = fmt.Sprintf("benchmark: first %d of %d frames · original %s", r.Frames, r.Total, FormatSize(r.Original))
	}
	out := []string{head, fmt.Sprintf("%-7s %-9s %-20s %-7s %s", "backend", "archive", "saved", "encode", "speed")}
	for _, row := range r.Rows {
		saved := fmt.Sprintf("%s (%.1f%%)", FormatSize(row.Saved()), row.SavedPct())
		ver := ""
		if row.Verified {
			ver = " ✓"
		}
		out = append(out, fmt.Sprintf("%-7s %-9s %-20s %-7s %.1f MB/s%s",
			row.Backend, FormatSize(row.Size), saved, benchDur(row.Encode), row.Speed(), ver))
		if row.Path != "" {
			out = append(out, "  kept: "+row.Path)
		}
	}
	return out
}

func Benchmark(indir string, opt BenchOptions) (*BenchReport, error) {
	for _, b := range opt.Backends {
		if b != "auto" && b != "go" && b != "ffmpeg" {
			return nil, fmt.Errorf("backend must be auto, go or ffmpeg, got %q", b)
		}
		if b == "ffmpeg" && !ff.Available() {
			return nil, fmt.Errorf("backend %q needs ffmpeg in PATH", b)
		}
	}
	all, err := ListFrames(indir)
	if err != nil {
		return nil, err
	}
	src := indir
	if opt.MaxFrames > 0 && len(all) > opt.MaxFrames {
		tmp, err := os.MkdirTemp("", "3dfc-bench-in-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		for _, p := range all[:opt.MaxFrames] {
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(tmp, filepath.Base(p)), b, 0644); err != nil {
				return nil, err
			}
		}
		src = tmp
	}
	files, err := ListFrames(src)
	if err != nil {
		return nil, err
	}
	var orig int64
	for _, p := range files {
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		orig += st.Size()
	}
	work, err := os.MkdirTemp("", "3dfc-bench-*")
	if err != nil {
		return nil, err
	}
	if !opt.Keep {
		defer os.RemoveAll(work)
	}
	rep := &BenchReport{Frames: len(files), Total: len(all), Original: orig}
	for _, b := range opt.Backends {
		o := DefaultOptions()
		o.Backend, o.Preset, o.GOPMax, o.Tile, o.FPS = b, opt.Preset, opt.GOPMax, opt.Tile, opt.FPS
		if o.Preset == "" {
			o.Preset = "max"
		}
		if o.GOPMax < 8 {
			o.GOPMax = 8
		}
		if o.Tile < 8 {
			o.Tile = 8
		}
		o.Progress = func(stage string, done, total int) {
			if opt.Progress != nil {
				opt.Progress(b, stage, done, total)
			}
		}
		out := filepath.Join(work, "bench-"+b+".3dfc")
		t0 := time.Now()
		if _, err := Create(src, out, o); err != nil {
			return nil, fmt.Errorf("backend %s: %w", b, err)
		}
		el := time.Since(t0)
		st, err := os.Stat(out)
		if err != nil {
			return nil, err
		}
		verified := false
		if opt.Verify {
			a, err := Open(out)
			if err != nil {
				return nil, err
			}
			if err := a.Verify(nil); err != nil {
				return nil, fmt.Errorf("backend %s: %w", b, err)
			}
			verified = true
		}
		path := ""
		if opt.Keep {
			path = out
		}
		rep.Rows = append(rep.Rows, BenchRow{
			Backend: b, Frames: len(files), Original: orig,
			Size: st.Size(), Encode: el, Verified: verified, Path: path,
		})
	}
	return rep, nil
}

func SplitBackends(s string) []string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "all" {
		out := []string{"auto", "go"}
		if ff.Available() {
			out = append(out, "ffmpeg")
		}
		return out
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
