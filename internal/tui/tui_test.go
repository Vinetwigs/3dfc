package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestMenuRenders(t *testing.T) {
	m := newModel()
	v := m.View()
	for _, want := range []string{"3DFC", "Archive", "Extract", "Benchmark", "Info", "Verify"} {
		if !strings.Contains(v, want) {
			t.Fatalf("menu missing %q", want)
		}
	}
	if strings.Contains(v, "COLD FRAME ARCHIVE") || strings.Contains(v, "WORKSPACE") {
		t.Fatal("home header contains removed branding")
	}
}

func TestMenuKeys(t *testing.T) {
	m := newModel()
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	if got.(model).scr != scrSingle {
		t.Fatal("key 4 should open info")
	}
	got, _ = newModel().Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if got.(model).scr != scrArchive {
		t.Fatal("key 1 should open archive")
	}
	got, _ = newModel().Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if got.(model).scr != scrBenchmark {
		t.Fatal("key 3 should open benchmark")
	}
	got, _ = newModel().Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	if got.(model).scr != scrSingle || got.(model).singleOp != "verify" {
		t.Fatal("key 5 should open verify")
	}
}

func TestArchiveValidationInline(t *testing.T) {
	m := newModel()
	m.scr = scrArchive
	got, _ := m.submitArchive()
	rm := got.(model)
	if rm.scr != scrArchive || rm.formErr == "" {
		t.Fatal("expected inline form error, same screen")
	}
	if !strings.Contains(rm.View(), "path is required") {
		t.Fatal("form view should show the error")
	}
}

func TestArchiveGopValidation(t *testing.T) {
	m := newModel()
	m.aForm[0].text.SetValue("in")
	m.aForm[1].text.SetValue("out")
	m.aForm[4].text.SetValue("3")
	got, _ := m.submitArchive()
	if got.(model).formErr == "" {
		t.Fatal("expected gop error")
	}
}

func TestExtractValidation(t *testing.T) {
	m := newModel()
	m.scr = scrExtract
	got, _ := m.submitExtract()
	if got.(model).formErr == "" {
		t.Fatal("expected inline error for empty paths")
	}
}

func TestOptionCycle(t *testing.T) {
	m := newModel()
	m.scr = scrArchive
	m.aFocus = 2
	if m.aForm[2].value() != "auto" {
		t.Fatal("default backend should be auto")
	}
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	rm := got.(model)
	if rm.aForm[2].value() != "go" {
		t.Fatalf("right should cycle to go, got %q", rm.aForm[2].value())
	}
	got, _ = rm.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got.(model).aForm[2].value() != "auto" {
		t.Fatal("left should cycle back to auto")
	}
}

func TestExtractSkipsInactiveFields(t *testing.T) {
	m := newModel()
	m.scr = scrExtract
	m.xFocus = 2
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got.(model).xFocus != 5 {
		t.Fatalf("all mode should skip range inputs and focus overwrite, focus = %d", got.(model).xFocus)
	}
	m.xForm[2].idx = 3
	v := m.View()
	if !strings.Contains(v, "To (incl.)") {
		t.Fatal("range mode should show inclusive end label")
	}
}

func TestInputArrowKeysRemainEditable(t *testing.T) {
	m := newModel()
	m.scr = scrArchive
	m.aForm[0].text.SetValue("abcdef")
	m.aFocus = 0
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got.(model).aForm[0].text.Value() != "abcdef" {
		t.Fatal("left arrow should move the caret, not change form state")
	}
}

func TestLayoutFitsCommonTerminal(t *testing.T) {
	for _, scr := range []screen{scrMenu, scrArchive, scrExtract, scrBenchmark, scrRun, scrResult} {
		m := newModel()
		m.width, m.height, m.scr = 80, 24, scr
		m.runTitle = "Archive"
		m.total = 100
		m.resTitle = "Archive"
		m.resLines = []string{"saved: 1.2 GB (50.0%)"}
		view := m.View()
		if h := lipgloss.Height(view); h > 24 {
			t.Fatalf("screen %d exceeds 24 rows (%d)", scr, h)
		}
		for i, line := range strings.Split(view, "\n") {
			if w := lipgloss.Width(line); w > 80 {
				t.Fatalf("screen %d line %d overflows 80 columns (%d): %q", scr, i, w, line)
			}
		}
	}
}

func TestSelectedRowsFillAvailableWidth(t *testing.T) {
	m := newModel()
	menu := m.viewMenu(60)
	found := false
	for _, line := range strings.Split(menu, "\n") {
		if strings.Contains(line, "Archive") {
			found = true
			if w := lipgloss.Width(line); w != 60 {
				t.Fatalf("selected home row width = %d, want 60", w)
			}
		}
	}
	if !found {
		t.Fatal("selected home row not rendered")
	}
	for _, value := range []string{"", "C:\\renders\\shot01"} {
		m.aForm[0].text.SetValue(value)
		line := m.fieldRow("Frames", m.aForm[0].text.View(), true, true, 60)
		if w := lipgloss.Width(line); w != 60 {
			t.Fatalf("selected input row width with value %q = %d, want 60", value, w)
		}
	}
}

func TestRunAndResultFlow(t *testing.T) {
	m := newModel()
	m.scr = scrRun
	m.ch = make(chan tea.Msg, 4)
	v := m.View()
	if !strings.Contains(v, "0%") {
		t.Fatalf("run screen missing percent: %q", v)
	}
	got, _ := m.Update(progressMsg{stage: "encode", done: 1, total: 2, pct: 0.5})
	rm := got.(model)
	if rm.pct != 0.5 {
		t.Fatalf("pct = %v, want 0.5", rm.pct)
	}
	if !strings.Contains(rm.View(), "1 / 2") {
		t.Fatal("run screen should show counts")
	}
	if !strings.Contains(rm.View(), "ENCODE") {
		t.Fatal("run screen should identify the current phase")
	}
	got2, _ := rm.Update(doneMsg{title: "Verify", lines: []string{"verify ok: 3 frames intact"}})
	rm2 := got2.(model)
	if rm2.scr != scrResult {
		t.Fatal("expected result screen after done")
	}
	if !strings.Contains(rm2.View(), "verify ok") {
		t.Fatal("result screen missing summary")
	}
}

func TestVerifyShortcut(t *testing.T) {
	m := newModel()
	m.scr = scrResult
	m.resTitle = "Archive"
	m.lastArchive = "x.3dfc"
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if got.(model).scr != scrRun {
		t.Fatal("v should start verify run")
	}
}

func TestFormsRender(t *testing.T) {
	m := newModel()
	m.scr = scrArchive
	if v := m.View(); !strings.Contains(v, "Backend") || !strings.Contains(v, "auto") || strings.Count(v, "Overwrite") != 1 || !strings.Contains(v, "Resume") {
		t.Fatalf("archive form wrong:\n%s", v)
	}
	m.scr = scrExtract
	if v := m.View(); !strings.Contains(v, "Mode") || !strings.Contains(v, "all") || strings.Count(v, "Overwrite") != 1 {
		t.Fatalf("extract form wrong:\n%s", v)
	}
	m.scr = scrBenchmark
	m.bForm[0].text.SetValue("shots")
	if v := m.View(); !strings.Contains(v, "BENCHMARK") || !strings.Contains(v, "Backends") {
		t.Fatalf("benchmark form wrong:\n%s", v)
	}
	m.scr = scrSingle
	m.singleOp = "verify"
	if !strings.Contains(m.View(), "Verify") {
		t.Fatal("single screen missing title")
	}
}

func TestBenchmarkValidation(t *testing.T) {
	m := newModel()
	m.scr = scrBenchmark
	got, _ := m.submitBenchmark()
	if got.(model).formErr == "" {
		t.Fatal("expected inline error for empty source")
	}
	m.bForm[0].text.SetValue("frames")
	m.bForm[4].text.SetValue("2")
	got, _ = m.submitBenchmark()
	if !strings.Contains(got.(model).formErr, "gop") {
		t.Fatal("expected benchmark GOP validation")
	}
}

func TestSavedEmphasis(t *testing.T) {
	m := newModel()
	m.scr = scrResult
	m.resTitle = "Archive"
	m.resLines = []string{"original: 10 MB", "archive: 4 MB", "saved: 6 MB (60.0%)"}
	if !strings.Contains(m.View(), "saved: 6 MB") {
		t.Fatal("result should show savings")
	}
}
