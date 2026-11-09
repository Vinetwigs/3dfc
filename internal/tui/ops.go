package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"3dfc/internal/archive"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func cleanUserPath(value string) (string, error) {
	p := strings.ReplaceAll(strings.TrimSpace(value), "\x00", "")
	p = strings.TrimSpace(p)
	if len(p) >= 2 && ((p[0] == '"' && p[len(p)-1] == '"') || (p[0] == '\'' && p[len(p)-1] == '\'')) {
		p = strings.TrimSpace(p[1 : len(p)-1])
	}
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	return filepath.Clean(p), nil
}

func (m model) fail(err error) (tea.Model, tea.Cmd) {
	m.formErr = err.Error()
	return m, nil
}

func (m model) startRun(title string, op func(ch chan tea.Msg)) (tea.Model, tea.Cmd) {
	m.scr = scrRun
	m.runTitle = title
	m.stage = ""
	m.pct = 0
	m.done, m.total = 0, 0
	m.elapsed = 0
	m.runStart = time.Now()
	bar, err := progress.NewModel(progress.WithSolidFill("#FF0000"), progress.WithWidth(m.barWidth), progress.WithoutPercentage())
	if err == nil {
		m.bar = *bar
	}
	m.ch = make(chan tea.Msg, 64)
	ch := m.ch
	go op(ch)
	tick := tea.Tick(m.spin.Spinner.FPS, func(time.Time) tea.Msg { return spinner.Tick() })
	return m, tea.Batch(tick, waitCh(ch))
}

func (m model) startVerify(path string) (tea.Model, tea.Cmd) {
	return m.startRun("Verify", func(ch chan tea.Msg) {
		a, err := archive.Open(path)
		if err != nil {
			ch <- doneMsg{title: "Verify", err: fmt.Errorf("open archive %q: %w", path, err)}
			return
		}
		n := a.Count()
		err = a.Verify(func(d, t int) {
			ch <- progressMsg{stage: "verify", done: d, total: t, pct: phasePercent(d, t)}
		})
		if err != nil {
			ch <- doneMsg{title: "Verify", err: err}
			return
		}
		ch <- doneMsg{title: "Verify", lines: []string{fmt.Sprintf("verify ok: %d frames intact", n)}}
	})
}

func parsePositive(s, what string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < 0 {
		return 0, fmt.Errorf("bad %s %q", what, s)
	}
	return v, nil
}

func (m model) submitArchive() (tea.Model, tea.Cmd) {
	v := func(i int) string { return m.aForm[i].value() }
	in, out, backend, preset := v(0), v(1), v(2), v(3)
	var err error
	in, err = cleanUserPath(in)
	if err != nil {
		return m.fail(fmt.Errorf("input: %w", err))
	}
	out, err = cleanUserPath(out)
	if err != nil {
		return m.fail(fmt.Errorf("output: %w", err))
	}
	gop, err := parsePositive(v(4), "gop")
	if err != nil || gop < 8 {
		return m.fail(fmt.Errorf("gop must be >= 8"))
	}
	tile, err := parsePositive(v(5), "tile")
	if err != nil || tile < 8 {
		return m.fail(fmt.Errorf("tile must be >= 8"))
	}
	fps, err := parsePositive(v(6), "fps")
	if err != nil || fps < 1 {
		return m.fail(fmt.Errorf("fps must be >= 1"))
	}
	overwrite := v(7) == "yes"
	resume := v(8) == "yes"
	m.lastArchive = out
	return m.startRun("Archive", func(ch chan tea.Msg) {
		opt := archive.DefaultOptions()
		opt.Backend, opt.Preset, opt.GOPMax, opt.Tile, opt.FPS = backend, preset, gop, tile, fps
		opt.Overwrite = overwrite
		opt.Resume = resume
		opt.Progress = func(stage string, d, t int) {
			p := phasePercent(d, t)
			if stage == "scan" {
				p *= 0.5
			} else {
				p = 0.5 + p*0.5
			}
			ch <- progressMsg{stage: stage, done: d, total: t, pct: p}
		}
		rep, err := archive.Create(in, out, opt)
		if err != nil {
			ch <- doneMsg{title: "Archive", err: err}
			return
		}
		lines := rep.Lines()
		for _, w := range rep.Warnings {
			lines = append(lines, "warning: "+w)
		}
		ch <- doneMsg{title: "Archive", lines: lines}
	})
}

func (m model) submitExtract() (tea.Model, tea.Cmd) {
	v := func(i int) string { return m.xForm[i].value() }
	in, out, mode, overwrite := v(0), v(1), v(2), v(5) == "yes"
	var err error
	in, err = cleanUserPath(in)
	if err != nil {
		return m.fail(fmt.Errorf("archive: %w", err))
	}
	out, err = cleanUserPath(out)
	if err != nil {
		return m.fail(fmt.Errorf("output: %w", err))
	}
	v1s, v2s := v(3), v(4)
	return m.startRun("Extract", func(ch chan tea.Msg) {
		a, err := archive.Open(in)
		if err != nil {
			ch <- doneMsg{title: "Extract", err: err}
			return
		}
		n := a.Count()
		lo, hi := 0, n-1
		switch mode {
		case "frame":
			f, err := parsePositive(v1s, "frame")
			if err != nil {
				ch <- doneMsg{title: "Extract", err: err}
				return
			}
			lo, hi = f, f
		case "upto":
			u, err := parsePositive(v1s, "upto")
			if err != nil {
				ch <- doneMsg{title: "Extract", err: err}
				return
			}
			lo, hi = 0, u
		case "range":
			f, err := parsePositive(v1s, "from")
			if err != nil {
				ch <- doneMsg{title: "Extract", err: err}
				return
			}
			t, err := parsePositive(v2s, "to")
			if err != nil {
				ch <- doneMsg{title: "Extract", err: err}
				return
			}
			lo, hi = f, t
		}
		if err := a.CheckRange(lo, hi); err != nil {
			ch <- doneMsg{title: "Extract", err: err}
			return
		}
		err = a.ExtractRangeWithOptions(lo, hi, out, archive.ExtractOptions{Overwrite: overwrite}, func(d, t int) {
			ch <- progressMsg{stage: "restore", done: d, total: t, pct: phasePercent(d, t)}
		})
		if err != nil {
			ch <- doneMsg{title: "Extract", err: err}
			return
		}
		ch <- doneMsg{title: "Extract", lines: []string{fmt.Sprintf("frames %d..%d -> %s", lo, hi, out)}}
	})
}

func (m model) submitBenchmark() (tea.Model, tea.Cmd) {
	v := func(i int) string { return m.bForm[i].value() }
	src, backs, preset, maxs := v(0), v(1), v(2), v(3)
	var err error
	src, err = cleanUserPath(src)
	if err != nil {
		return m.fail(fmt.Errorf("source: %w", err))
	}
	backends := archive.SplitBackends(backs)
	max := 0
	if maxs != "" {
		var err error
		max, err = parsePositive(maxs, "max")
		if err != nil || max < 1 {
			return m.fail(fmt.Errorf("max must be >= 1"))
		}
	}
	gop, err := parsePositive(v(4), "gop")
	if err != nil || gop < 8 {
		return m.fail(fmt.Errorf("gop must be >= 8"))
	}
	tile, err := parsePositive(v(5), "tile")
	if err != nil || tile < 8 {
		return m.fail(fmt.Errorf("tile must be >= 8"))
	}
	fps, err := parsePositive(v(6), "fps")
	if err != nil || fps < 1 {
		return m.fail(fmt.Errorf("fps must be >= 1"))
	}
	return m.startRun("Benchmark", func(ch chan tea.Msg) {
		files, err := archive.ListFrames(src)
		if err != nil {
			ch <- doneMsg{title: "Benchmark", err: err}
			return
		}
		n := len(files)
		if max > 0 && n > max {
			n = max
		}
		tot := 2 * len(backends) * n
		seen := map[string]map[string]int{}
		opt := archive.BenchOptions{
			Preset: preset, GOPMax: gop, Tile: tile, FPS: fps,
			Backends: backends, MaxFrames: max,
			Verify: v(7) == "yes", Keep: v(8) == "yes",
		}
		opt.Progress = func(tag, stage string, d, _ int) {
			if seen[tag] == nil {
				seen[tag] = make(map[string]int)
			}
			seen[tag][stage] = d
			sum := 0
			for _, b := range backends {
				sum += seen[b]["scan"] + seen[b]["encode"]
			}
			ch <- progressMsg{
				stage: fmt.Sprintf("%s · %s", strings.ToUpper(tag), strings.ToUpper(stage)),
				done:  d,
				total: len(files),
				pct:   float64(sum) / float64(tot),
			}
		}
		rep, err := archive.Benchmark(src, opt)
		if err != nil {
			ch <- doneMsg{title: "Benchmark", err: err}
			return
		}
		ch <- doneMsg{title: "Benchmark", lines: rep.Lines()}
	})
}

func phasePercent(done, total int) float64 {
	if total <= 0 {
		return 0
	}
	p := float64(done) / float64(total)
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}
