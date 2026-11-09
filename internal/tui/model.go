package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type screen int

const (
	scrMenu screen = iota
	scrArchive
	scrExtract
	scrBenchmark
	scrSingle
	scrRun
	scrResult
)

type progressMsg struct {
	stage       string
	done, total int
	pct         float64
}

type doneMsg struct {
	title string
	lines []string
	err   error
}

type field struct {
	label string
	text  textinput.Model
	opts  []string
	idx   int
	opt   bool
}

func textField(label, ph, def string) field {
	t := textinput.NewModel()
	t.Width = 36
	t.Placeholder = ph
	t.SetValue(def)
	t.Prompt = "› "
	t.PromptStyle = cursorStyle
	t.TextStyle = metricValueStyle
	t.PlaceholderStyle = dimStyle
	return field{label: label, text: t}
}

func styleTextInput(t *textinput.Model, focused bool) {
	if focused {
		t.PromptStyle = focusedPromptStyle
		t.TextStyle = focusedInputStyle
		t.PlaceholderStyle = focusedHintStyle
		return
	}
	t.PromptStyle = cursorStyle
	t.TextStyle = metricValueStyle
	t.PlaceholderStyle = dimStyle
}

func optField(label string, opts []string, def int) field {
	return field{label: label, opts: opts, idx: def, opt: true}
}

func (f field) value() string {
	if f.opt {
		return f.opts[f.idx]
	}
	return strings.TrimSpace(f.text.Value())
}

type model struct {
	scr         screen
	menu        []string
	menuIdx     int
	aForm       []field
	aFocus      int
	xForm       []field
	xFocus      int
	bForm       []field
	bFocus      int
	single      textinput.Model
	singleOp    string
	formErr     string
	runTitle    string
	stage       string
	bar         progress.Model
	barWidth    int
	spin        spinner.Model
	pct         float64
	done        int
	total       int
	runStart    time.Time
	elapsed     time.Duration
	ch          chan tea.Msg
	resTitle    string
	resLines    []string
	resErr      error
	lastArchive string
	width       int
	height      int
}

func Run() error {
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel() model {
	aForm := []field{
		textField("Frames", "input folder", ""),
		textField("Save as", "output .3dfc", ""),
		optField("Backend", []string{"auto", "go", "ffmpeg"}, 0),
		optField("Preset", []string{"max", "fast"}, 0),
		textField("GOP", "≥ 8", "300"),
		textField("Tile", "≥ 8", "64"),
		textField("FPS", "≥ 1", "24"),
		optField("Overwrite", []string{"no", "yes"}, 0),
		optField("Resume", []string{"no", "yes"}, 0),
	}
	xForm := []field{
		textField("Archive", "archive .3dfc", ""),
		textField("Output", "output folder", ""),
		optField("Mode", []string{"all", "frame", "upto", "range"}, 0),
		textField("Value", "frame / upto / from", ""),
		textField("To", "range end", ""),
		optField("Overwrite", []string{"no", "yes"}, 0),
	}
	bForm := []field{
		textField("Frames", "input folder", ""),
		optField("Backends", []string{"all", "auto", "go", "ffmpeg"}, 0),
		optField("Preset", []string{"max", "fast"}, 0),
		textField("Max", "first N frames, empty = all", ""),
		textField("GOP", "≥ 8", "300"),
		textField("Tile", "≥ 8", "64"),
		textField("FPS", "≥ 1", "24"),
		optField("Verify", []string{"yes", "no"}, 0),
		optField("Keep files", []string{"no", "yes"}, 0),
	}
	s := textinput.NewModel()
	s.Width = 36
	s.Placeholder = "archive .3dfc"
	s.Prompt = "› "
	s.PromptStyle = cursorStyle
	s.TextStyle = metricValueStyle
	s.PlaceholderStyle = dimStyle
	sp := spinner.NewModel()
	sp.Spinner = spinner.Dot
	sp.Style = cursorStyle
	setInputWidths(aForm, 66)
	setInputWidths(xForm, 66)
	setInputWidths(bForm, 66)
	bar, _ := progress.NewModel(progress.WithSolidFill("#FF0000"), progress.WithWidth(40), progress.WithoutPercentage())
	return model{
		scr: scrMenu, menu: []string{"Archive", "Extract", "Benchmark", "Info", "Verify"},
		aForm: aForm, xForm: xForm, bForm: bForm, single: s,
		bar: *bar, barWidth: 40,
		spin: sp, width: 80, height: 24,
	}
}

func (m model) Init() tea.Cmd { return nil }

func waitCh(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (m *model) focusFields(fields []field, focus int) tea.Cmd {
	var cmds []tea.Cmd
	for i := range fields {
		if fields[i].opt {
			continue
		}
		if i == focus {
			styleTextInput(&fields[i].text, true)
			cmds = append(cmds, fields[i].text.Focus())
		} else {
			fields[i].text.Blur()
			styleTextInput(&fields[i].text, false)
		}
	}
	return tea.Batch(cmds...)
}

func (m *model) focusSingle() tea.Cmd {
	styleTextInput(&m.single, true)
	return m.single.Focus()
}

func activeField(screen screen, fields []field, index int) bool {
	if screen != scrExtract || len(fields) < 5 {
		return true
	}
	mode := fields[2].value()
	switch index {
	case 3:
		return mode != "all"
	case 4:
		return mode == "range"
	default:
		return true
	}
}

func (m model) nextField(fields []field, focus, direction int) int {
	for step := 0; step < len(fields); step++ {
		focus = (focus + direction + len(fields)) % len(fields)
		if activeField(m.scr, fields, focus) {
			return focus
		}
	}
	return focus
}

func setInputWidths(fields []field, width int) {
	inputWidth := width - 24
	if inputWidth < 12 {
		inputWidth = 12
	}
	if inputWidth > 48 {
		inputWidth = 48
	}
	for i := range fields {
		if !fields[i].opt {
			fields[i].text.Width = inputWidth
		}
	}
}

func (m model) rate() float64 {
	if m.elapsed < 500*time.Millisecond || m.pct <= 0 {
		return 0
	}
	return float64(m.done) / m.elapsed.Seconds()
}

func (m model) eta() string {
	if m.elapsed < 500*time.Millisecond || m.pct <= 0 || m.pct >= 1 {
		return "…"
	}
	s := int(m.elapsed.Seconds() * (1 - m.pct) / m.pct)
	if s >= 60 {
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%ds", s)
}
