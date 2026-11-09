package archive

import (
	"strings"
	"testing"
)

func TestNaturalFrameOrdering(t *testing.T) {
	names := []string{"frame10.png", "frame2.png", "frame01.png", "frame1.png"}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if naturalLess(names[j], names[i]) {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	want := []string{"frame1.png", "frame01.png", "frame2.png", "frame10.png"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("natural order = %v, want %v", names, want)
		}
	}
}

func TestSequenceWarningsReportGaps(t *testing.T) {
	files := []FrameFile{
		{Name: "shot_0001.png"},
		{Name: "shot_0002.png"},
		{Name: "shot_0004.png"},
		{Name: "other_0001.png"},
		{Name: "other_0002.png"},
	}
	warnings := sequenceWarnings(files)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `"shot_"`) || !strings.Contains(warnings[0], "1 number") {
		t.Fatalf("unexpected sequence warnings: %v", warnings)
	}
}

func TestSequenceWarningsReportDuplicateIndices(t *testing.T) {
	warnings := sequenceWarnings([]FrameFile{
		{Name: "frame_1.png"},
		{Name: "frame_01.png"},
		{Name: "frame_2.png"},
	})
	if len(warnings) != 1 || !strings.Contains(warnings[0], "duplicate frame numbers") {
		t.Fatalf("unexpected duplicate warnings: %v", warnings)
	}
}
