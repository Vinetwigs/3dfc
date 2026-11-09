package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m model) View() string {
	w, h := m.width, m.height
	if w < 24 {
		w = 80
	}
	if h < 12 {
		h = 24
	}
	contentWidth := min(w-14, 84)
	if contentWidth < 24 {
		contentWidth = 24
	}
	body := m.viewBody(contentWidth)
	page := lipgloss.JoinVertical(lipgloss.Left,
		m.header(contentWidth),
		body,
		m.footer(contentWidth),
	)
	if w < 48 {
		return lipgloss.Place(w, h, lipgloss.Left, lipgloss.Top, page)
	}
	panel := panelStyle.Width(contentWidth + 6).Render(page)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, panel)
}

func (m model) header(width int) string {
	brand := logoStyle.Render("3DFC")
	section := sectionStyle.Render(strings.ToUpper(m.crumb()))
	if width < lipgloss.Width(brand)+lipgloss.Width(section)+2 {
		brand = logoStyle.Render("3DFC")
	}
	leftWidth := lipgloss.Width(brand)
	gap := width - leftWidth - lipgloss.Width(section)
	if gap < 1 {
		gap = 1
	}
	top := brand + strings.Repeat(" ", gap) + section
	return lipgloss.JoinVertical(lipgloss.Left, top, hr(width, red))
}

func (m model) crumb() string {
	switch m.scr {
	case scrArchive:
		return "Archive"
	case scrExtract:
		return "Extract"
	case scrBenchmark:
		return "Benchmark"
	case scrSingle:
		if m.singleOp == "verify" {
			return "Verify"
		}
		return "Info"
	case scrRun:
		return m.runTitle
	case scrResult:
		return "Complete"
	default:
		return "Home"
	}
}

func (m model) footer(width int) string {
	var keys []string
	switch m.scr {
	case scrMenu:
		keys = []string{chip("UP/DOWN", "navigate"), chip("1-5", "jump"), chip("ENTER", "select"), chip("Q", "quit")}
	case scrArchive, scrExtract, scrBenchmark:
		keys = []string{chip("TAB", "next"), chip("LEFT/RIGHT", "change"), chip("ENTER", "continue"), chip("ESC", "back")}
	case scrSingle:
		keys = []string{chip("ENTER", "run"), chip("ESC", "back")}
	case scrRun:
		keys = []string{chip("CTRL+C", "quit")}
	case scrResult:
		if m.resErr == nil && m.resTitle == "Archive" && m.lastArchive != "" {
			keys = []string{chip("V", "verify"), chip("ESC", "menu"), chip("Q", "quit")}
		} else {
			keys = []string{chip("ESC", "menu"), chip("Q", "quit")}
		}
	}
	line := strings.Join(keys, dimStyle.Render("   "))
	if lipgloss.Width(line) > width {
		line = strings.Join(keys, "\n")
	}
	return lipgloss.JoinVertical(lipgloss.Left, hr(width, red), line)
}

func (m model) viewBody(width int) string {
	switch m.scr {
	case scrArchive:
		return m.viewForm("Archive frames", "CREATE ARCHIVE", m.aForm, m.aFocus, nil, width)
	case scrExtract:
		mode := m.xForm[2].value()
		fields := append([]field(nil), m.xForm...)
		switch mode {
		case "frame":
			fields[3].label = "Frame"
		case "upto":
			fields[3].label = "Through"
		case "range":
			fields[3].label = "From"
			fields[4].label = "To (incl.)"
		}
		active := func(i int) bool {
			switch i {
			case 3:
				return mode != "all"
			case 4:
				return mode == "range"
			default:
				return true
			}
		}
		return m.viewForm("Restore frames", "RESTORE", fields, m.xFocus, active, width)
	case scrBenchmark:
		return m.viewForm("Compare compression", "BENCHMARK", m.bForm, m.bFocus, nil, width)
	case scrSingle:
		title := "Archive information"
		if m.singleOp == "verify" {
			title = "Verify archive"
		}
		return m.viewSingle(title, width)
	case scrRun:
		return m.viewRun(width)
	case scrResult:
		return m.viewResult(width)
	default:
		return m.viewMenu(width)
	}
}

func (m model) viewMenu(width int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Choose an action"))
	b.WriteString("\n\n")
	for i, item := range m.menu {
		key := menuKeyStyle.Render(fmt.Sprintf("%d", i+1))
		label := menuLabelStyle.Render(item)
		row := "  " + key + "   " + label
		if i == m.menuIdx {
			row = fullBackgroundLine(fmt.Sprintf("›  %d   %s", i+1, item), width, menuSelectedStyle)
		}
		b.WriteString(row)
		if i < len(m.menu)-1 {
			b.WriteString("\n\n")
		}
	}
	return lipgloss.NewStyle().Padding(1, 0, 2, 0).Render(b.String())
}

func (m model) viewForm(title, tag string, fields []field, focus int, active func(i int) bool, width int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(title))
	b.WriteString(dimStyle.Render("  /  " + tag))
	b.WriteString("\n\n")
	for i, f := range fields {
		group := ""
		switch tag {
		case "CREATE ARCHIVE":
			if i == 2 {
				group = "ENCODING"
			} else if i == 7 {
				group = "SAFETY"
			}
		case "RESTORE":
			if i == 2 {
				group = "SELECTION"
			} else if i == 5 {
				group = "OUTPUT"
			}
		case "BENCHMARK":
			if i == 1 {
				group = "TRIAL"
			} else if i == 4 {
				group = "SETTINGS"
			} else if i == 7 {
				group = "OUTPUT"
			}
		}
		if group != "" {
			b.WriteString(dimStyle.Render(group) + "\n")
		}
		on := active == nil || active(i)
		control := f.text.View()
		if f.opt {
			control = m.viewOpt(f, i == focus && on)
		}
		row := m.fieldRow(f.label, control, i == focus, on, width)
		b.WriteString(row)
		if i < len(fields)-1 {
			b.WriteString("\n")
		}
	}
	if m.formErr != "" {
		b.WriteString("\n\n" + errStyle.Render("!  "+m.formErr))
	}
	return lipgloss.NewStyle().Padding(1, 0, 1, 0).Render(b.String())
}

func (m model) fieldRow(label, control string, focused, active bool, width int) string {
	if !active {
		return inactiveRowStyle.Render(fmt.Sprintf("  %-10s -", label))
	}
	row := fmt.Sprintf("%-10s %s", label, control)
	if focused {
		lead := "› " + fmt.Sprintf("%-10s", label) + " "
		pad := width - lipgloss.Width(lead) - lipgloss.Width(control)
		if pad < 0 {
			pad = 0
		}
		return focusedRowStyle.Render(lead) + control + focusedRowStyle.Render(strings.Repeat(" ", pad))
	}
	return normalRowStyle.Render("  " + row)
}

func fullBackgroundLine(text string, width int, style lipgloss.Style) string {
	pad := width - lipgloss.Width(text)
	if pad > 0 {
		text += strings.Repeat(" ", pad)
	}
	return style.Render(text)
}

func (m model) viewOpt(f field, focused bool) string {
	if focused {
		return focusedInputStyle.Render("‹ " + f.opts[f.idx] + " ›")
	}
	return optionArrowStyle.Render("‹ ") + optionValueStyle.Render(f.opts[f.idx]) + optionArrowStyle.Render(" ›")
}

func (m model) viewSingle(title string, width int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")
	b.WriteString(m.fieldRow("Archive", m.single.View(), true, true, width))
	if m.formErr != "" {
		b.WriteString("\n\n" + errStyle.Render("!  "+m.formErr))
	}
	return lipgloss.NewStyle().Padding(1, 0, 2, 0).Render(b.String())
}

func (m model) viewRun(width int) string {
	percent := fmt.Sprintf("%3.0f%%", m.pct*100)
	barWidth := min(width-14, 64)
	if barWidth < 12 {
		barWidth = 12
	}
	bar := m.bar
	bar.Width = barWidth

	var b strings.Builder
	b.WriteString(titleStyle.Render(m.runTitle))
	b.WriteString("  ")
	b.WriteString(m.spin.View())
	if m.stage != "" {
		b.WriteString("  ")
		b.WriteString(metricLabelStyle.Render(strings.ToUpper(m.stage)))
	}
	b.WriteString("\n\n")
	b.WriteString(bar.View(m.pct))
	b.WriteString("  ")
	b.WriteString(percentStyle.Render(percent))
	b.WriteString("\n\n")
	b.WriteString(metricLabelStyle.Render("PROCESSED"))
	b.WriteString("  ")
	b.WriteString(metricValueStyle.Render(fmt.Sprintf("%s / %s", formatCount(m.done), formatCount(m.total))))
	b.WriteString("     ")
	b.WriteString(metricLabelStyle.Render("ELAPSED"))
	b.WriteString("  ")
	b.WriteString(metricValueStyle.Render(formatDur(m.elapsed)))
	if r := m.rate(); r > 0 {
		b.WriteString("     ")
		b.WriteString(metricLabelStyle.Render("ETA"))
		b.WriteString("  ")
		b.WriteString(metricValueStyle.Render(m.eta()))
	}
	return lipgloss.NewStyle().Padding(3, 0).Render(b.String())
}

func (m model) viewResult(width int) string {
	var b strings.Builder
	if m.resErr != nil {
		b.WriteString(resultErrorTitle.Render("FAILED"))
		b.WriteString("\n\n")
		b.WriteString(errStyle.Width(width - 4).Render(m.resErr.Error()))
		return lipgloss.NewStyle().Padding(2, 0).Render(b.String())
	}
	b.WriteString(resultSuccessTitle.Render("COMPLETE"))
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(m.resTitle))
	b.WriteString("\n\n")
	for i, line := range m.resLines {
		style := resultLineStyle
		if strings.HasPrefix(line, "saved:") {
			style = resultSavedStyle
		} else if strings.HasPrefix(line, "warning:") {
			style = warnStyle
		}
		b.WriteString(style.Width(width - 4).Render(line))
		if i < len(m.resLines)-1 {
			b.WriteString("\n")
		}
	}
	return lipgloss.NewStyle().Padding(2, 0).Render(b.String())
}

func formatCount(n int) string {
	if n < 0 {
		n = 0
	}
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
