package tui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"3dfc/internal/archive"
	tea "github.com/charmbracelet/bubbletea"
)

func TestInfoAcceptsPastedPathWithInterleavedNULs(t *testing.T) {
	source := t.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 20, G: 30, B: 40, A: 255})
	f, err := os.Create(filepath.Join(source, "frame 0001.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(t.TempDir(), "folder with spaces", "sample archive.3dfc")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Create(source, archivePath, archive.DefaultOptions()); err != nil {
		t.Fatal(err)
	}

	m := newModel()
	m.scr = scrSingle
	m.singleOp = "info"
	m.focusSingle()
	var pasted []rune
	for _, r := range "\"" + archivePath + "\"" {
		pasted = append(pasted, r, 0)
	}
	m.single.SetValue(string(pasted))
	got, _ := m.updateSingle(tea.KeyMsg{Type: tea.KeyEnter})
	result := got.(model)
	if result.resErr != nil {
		t.Fatal(result.resErr)
	}
	if result.scr != scrResult || result.resTitle != "Info" {
		t.Fatalf("unexpected result state: %+v", result)
	}
}

func TestCleanUserPath(t *testing.T) {
	path := filepath.Join("folder with spaces", "archive.3dfc")
	got, err := cleanUserPath("  \"" + path + "\"  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(path) {
		t.Fatalf("clean path = %q, want %q", got, filepath.Clean(path))
	}
	got, err = cleanUserPath("folder\x00 with\x00 spaces/archive.3dfc")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Clean("folder with spaces/archive.3dfc"); got != want {
		t.Fatalf("NUL-cleaned path = %q, want %q", got, want)
	}
	if _, err := cleanUserPath("\x00"); err == nil {
		t.Fatal("expected empty path validation error")
	}
}

func TestSingleFormErrorStaysUntilValueChanges(t *testing.T) {
	m := newModel()
	m.scr = scrSingle
	m.singleOp = "info"
	m.focusSingle()
	got, _ := m.updateSingle(tea.KeyMsg{Type: tea.KeyEnter})
	m = got.(model)
	if m.formErr == "" {
		t.Fatal("expected missing path error")
	}
	got, _ = m.updateSingle(tea.KeyMsg{Type: tea.KeyLeft})
	m = got.(model)
	if m.formErr == "" {
		t.Fatal("cursor movement must not clear validation error")
	}
	got, _ = m.updateSingle(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if got.(model).formErr != "" {
		t.Fatal("editing the value should clear validation error")
	}
}
