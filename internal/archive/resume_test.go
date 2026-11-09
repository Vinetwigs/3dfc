package archive

import (
	"os"
	"path/filepath"
	"testing"

	"3dfc/internal/ff"
)

func TestResumeAfterCommittedGOP(t *testing.T) {
	src := t.TempDir()
	makeFrames(t, src)
	files, err := ListFramesInfo(src)
	if err != nil {
		t.Fatal(err)
	}
	opt := DefaultOptions()
	opt.Backend = "go"
	opt.Preset = "fast"
	opt.GOPMax = 8
	opt.Resume = true
	segs, _, w, h, original, alpha, _, fingerprint, spills, err := scanFrames(files, opt, func(int) {})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, p := range spills {
			os.RemoveAll(p)
		}
	}()
	if len(segs) < 2 {
		t.Fatalf("test needs multiple GOPs, found %d", len(segs))
	}
	out := filepath.Join(t.TempDir(), "resumable.3dfc")
	hdr := makeArchiveHeader(w, h, len(files), original, opt, alpha)
	work, err := openArchiveWork(out, opt, fingerprint, checkpointConfig(opt, ff.Available()), hdr)
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]entry, 0, len(files))
	checkpoint := func(next, start int) error { return work.CommitSegment(next, entries[start:]) }
	if err := assemble(work.file, segs[:1], opt, false, &entries, len(files), 0, checkpoint); err != nil {
		work.Close(false)
		t.Fatal(err)
	}
	work.Close(false)
	noResume := opt
	noResume.Resume = false
	if _, err := Create(src, out, noResume); err == nil {
		t.Fatal("expected incomplete checkpoint protection without -resume")
	}

	var stages []string
	opt.Progress = func(stage string, _, _ int) {
		if len(stages) == 0 || stages[len(stages)-1] != stage {
			stages = append(stages, stage)
		}
	}
	rep, err := Create(src, out, opt)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Frames != len(files) {
		t.Fatalf("resumed archive has %d frames, want %d", rep.Frames, len(files))
	}
	a, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(nil); err != nil {
		t.Fatal(err)
	}
	if len(stages) < 2 || stages[0] != "scan" || stages[1] != "encode" {
		t.Fatalf("unexpected progress phases after resume: %v", stages)
	}
	paths := pathsForOutput(out)
	for _, p := range []string{paths.partial, paths.journal, paths.state} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("resume artifact remains after completion: %s", p)
		}
	}
}

func TestCreateProgressReportsPhases(t *testing.T) {
	src := t.TempDir()
	makeFrames(t, src)
	var phases []string
	last := ""
	opt := DefaultOptions()
	opt.Backend = "go"
	opt.Preset = "fast"
	opt.GOPMax = 8
	opt.Progress = func(stage string, done, total int) {
		if total != 16 || done < 0 || done > total {
			t.Fatalf("invalid %s progress %d/%d", stage, done, total)
		}
		if stage != last {
			phases = append(phases, stage)
			last = stage
		}
	}
	if _, err := Create(src, filepath.Join(t.TempDir(), "progress.3dfc"), opt); err != nil {
		t.Fatal(err)
	}
	if len(phases) != 2 || phases[0] != "scan" || phases[1] != "encode" {
		t.Fatalf("progress phases = %v, want [scan encode]", phases)
	}
}
