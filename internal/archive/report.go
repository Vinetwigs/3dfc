package archive

import "fmt"

type Report struct {
	Frames   int
	Width    int
	Height   int
	Original int64
	Size     int64
	Warnings []string
}

func (r *Report) Saved() int64 {
	return r.Original - r.Size
}

func (r *Report) SavedPct() float64 {
	if r.Original <= 0 {
		return 0
	}
	return 100 * float64(r.Saved()) / float64(r.Original)
}

func FormatSize(n int64) string {
	neg := ""
	if n < 0 {
		neg = "-"
		n = -n
	}
	f := float64(n)
	switch {
	case f >= 1e9:
		return neg + fmt.Sprintf("%.2f GB", f/1e9)
	case f >= 1e6:
		return neg + fmt.Sprintf("%.1f MB", f/1e6)
	case f >= 1e3:
		return neg + fmt.Sprintf("%.0f KB", f/1e3)
	default:
		return neg + fmt.Sprintf("%d B", n)
	}
}

func SavingsLines(original, size int64) []string {
	if original <= 0 {
		return []string{"archive: " + FormatSize(size), "saved: n/a"}
	}
	saved := original - size
	pct := 0.0
	if original > 0 {
		pct = 100 * float64(saved) / float64(original)
	}
	return []string{
		"original: " + FormatSize(original),
		"archive: " + FormatSize(size),
		fmt.Sprintf("saved: %s (%.1f%%)", FormatSize(saved), pct),
	}
}

func (r *Report) Lines() []string {
	out := []string{fmt.Sprintf("frames: %d (%dx%d)", r.Frames, r.Width, r.Height)}
	return append(out, SavingsLines(r.Original, r.Size)...)
}
