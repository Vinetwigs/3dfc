package tui

import (
	"fmt"
	"strconv"
	"time"

	"3dfc/internal/archive"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		contentWidth := min(msg.Width-14, 84)
		if contentWidth < 24 {
			contentWidth = 24
		}
		setInputWidths(m.aForm, contentWidth)
		setInputWidths(m.xForm, contentWidth)
		setInputWidths(m.bForm, contentWidth)
		m.single.Width = min(max(contentWidth-24, 12), 48)
		m.barWidth = contentWidth - 14
		if m.barWidth > 64 {
			m.barWidth = 64
		}
		if m.barWidth < 20 {
			m.barWidth = 20
		}
		m.bar.Width = m.barWidth
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.scr == scrMenu || m.scr == scrResult {
				return m, tea.Quit
			}
		case "esc":
			if m.scr != scrRun {
				m.formErr = ""
				m.scr = scrMenu
				return m, nil
			}
		case "v":
			if m.scr == scrResult && m.resErr == nil && m.resTitle == "Archive" && m.lastArchive != "" {
				return m.startVerify(m.lastArchive)
			}
		}
	case progressMsg:
		if msg.total > 0 {
			m.stage = msg.stage
			m.done, m.total = msg.done, msg.total
			m.pct = msg.pct
			m.elapsed = time.Since(m.runStart)
		}
		return m, waitCh(m.ch)
	case doneMsg:
		m.resTitle = msg.title
		m.resErr = msg.err
		m.resLines = msg.lines
		if msg.err == nil && !m.runStart.IsZero() {
			m.resLines = append(m.resLines, "time: "+formatDur(time.Since(m.runStart)))
		}
		m.scr = scrResult
		return m, nil
	}

	switch m.scr {
	case scrMenu:
		return m.updateMenu(msg)
	case scrArchive, scrExtract, scrBenchmark:
		return m.updateFields(msg)
	case scrSingle:
		return m.updateSingle(msg)
	case scrRun:
		if _, ok := msg.(spinner.TickMsg); ok {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil
	case scrResult:
		return m, nil
	}
	return m, nil
}

func (m model) updateMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "up":
			m.menuIdx--
			if m.menuIdx < 0 {
				m.menuIdx = len(m.menu) - 1
			}
			return m, nil
		case "down":
			m.menuIdx++
			if m.menuIdx >= len(m.menu) {
				m.menuIdx = 0
			}
			return m, nil
		case "1", "2", "3", "4", "5":
			n, _ := strconv.Atoi(k.String())
			if n >= 1 && n <= len(m.menu) {
				return m.openMenu(m.menu[n-1])
			}
			return m, nil
		case "enter":
			return m.openMenu(m.menu[m.menuIdx])
		}
	}
	return m, nil
}

func (m model) openMenu(item string) (tea.Model, tea.Cmd) {
	m.formErr = ""
	switch item {
	case "Archive":
		m.scr = scrArchive
		m.aFocus = 0
		return m, m.focusFields(m.aForm, 0)
	case "Extract":
		m.scr = scrExtract
		m.xFocus = 0
		return m, m.focusFields(m.xForm, 0)
	case "Benchmark":
		m.scr = scrBenchmark
		m.bFocus = 0
		return m, m.focusFields(m.bForm, 0)
	case "Info":
		m.scr = scrSingle
		m.singleOp = "info"
		return m, m.focusSingle()
	case "Verify":
		m.scr = scrSingle
		m.singleOp = "verify"
		return m, m.focusSingle()
	}
	return m, nil
}

func (m model) fieldsOf() ([]field, int) {
	switch m.scr {
	case scrArchive:
		return m.aForm, m.aFocus
	case scrExtract:
		return m.xForm, m.xFocus
	default:
		return m.bForm, m.bFocus
	}
}

func (m *model) setFields(fields []field, focus int) {
	switch m.scr {
	case scrArchive:
		m.aForm, m.aFocus = fields, focus
	case scrExtract:
		m.xForm, m.xFocus = fields, focus
	default:
		m.bForm, m.bFocus = fields, focus
	}
}

func (m model) updateFields(msg tea.Msg) (tea.Model, tea.Cmd) {
	fields, focus := m.fieldsOf()
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "up", "shift+tab":
			focus = m.nextField(fields, focus, -1)
			m.setFields(fields, focus)
			return m, m.focusFields(fields, focus)
		case "down", "tab":
			focus = m.nextField(fields, focus, 1)
			m.setFields(fields, focus)
			return m, m.focusFields(fields, focus)
		case "left", "right":
			if fields[focus].opt {
				d := 1
				if k.String() == "left" {
					d = -1
				}
				n := len(fields[focus].opts)
				fields[focus].idx = (fields[focus].idx + d + n) % n
				if m.scr == scrExtract && !activeField(m.scr, fields, focus) {
					focus = m.nextField(fields, focus, 1)
				}
				m.setFields(fields, focus)
				m.formErr = ""
				return m, m.focusFields(fields, focus)
			}
			var cmd tea.Cmd
			before := fields[focus].text.Value()
			fields[focus].text, cmd = fields[focus].text.Update(msg)
			m.setFields(fields, focus)
			if fields[focus].text.Value() != before {
				m.formErr = ""
			}
			return m, cmd
		case "enter":
			last := focus
			for i := focus + 1; i < len(fields); i++ {
				if activeField(m.scr, fields, i) {
					last = i
				}
			}
			if focus == last {
				m.setFields(fields, focus)
				switch m.scr {
				case scrArchive:
					return m.submitArchive()
				case scrExtract:
					return m.submitExtract()
				default:
					return m.submitBenchmark()
				}
			}
			focus = m.nextField(fields, focus, 1)
			m.setFields(fields, focus)
			return m, m.focusFields(fields, focus)
		default:
			if fields[focus].opt {
				return m, nil
			}
			var cmd tea.Cmd
			before := fields[focus].text.Value()
			fields[focus].text, cmd = fields[focus].text.Update(msg)
			m.setFields(fields, focus)
			if fields[focus].text.Value() != before {
				m.formErr = ""
			}
			return m, cmd
		}
	}
	if !fields[focus].opt {
		var cmd tea.Cmd
		before := fields[focus].text.Value()
		fields[focus].text, cmd = fields[focus].text.Update(msg)
		m.setFields(fields, focus)
		if fields[focus].text.Value() != before {
			m.formErr = ""
		}
		return m, cmd
	}
	return m, nil
}

func (m model) updateSingle(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.String() == "enter" {
		path, err := cleanUserPath(m.single.Value())
		if err != nil {
			m.formErr = err.Error()
			return m, nil
		}
		if m.singleOp == "info" {
			a, err := archive.Open(path)
			if err != nil {
				m.resTitle, m.resErr, m.resLines = "Info", fmt.Errorf("open archive %q: %w", path, err), nil
			} else {
				m.resTitle, m.resErr, m.resLines = "Info", nil, a.Summary()
			}
			m.scr = scrResult
			return m, nil
		}
		return m.startVerify(path)
	}
	var cmd tea.Cmd
	before := m.single.Value()
	m.single, cmd = m.single.Update(msg)
	if m.single.Value() != before {
		m.formErr = ""
	}
	return m, cmd
}

func formatDur(d time.Duration) string {
	s := int(d.Seconds())
	if s >= 60 {
		return strconv.Itoa(s/60) + "m" + strconv.Itoa(s%60) + "s"
	}
	return strconv.Itoa(s) + "s"
}
