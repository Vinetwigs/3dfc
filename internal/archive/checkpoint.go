// Copyright (C) 2026 Vinetwigs
// SPDX-License-Identifier: GPL-3.0-only

package archive

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type resumeCheckpoint struct {
	Version     uint16 `json:"version"`
	Fingerprint string `json:"fingerprint"`
	Config      string `json:"config"`
	NextSegment int    `json:"next_segment"`
	DataOffset  int64  `json:"data_offset"`
	EntryCount  int    `json:"entry_count"`
}

type checkpointPaths struct {
	partial string
	journal string
	state   string
}

type archiveWork struct {
	file       *os.File
	journal    *os.File
	paths      checkpointPaths
	checkpoint resumeCheckpoint
	entries    []entry
	start      int
	tempPath   string
	resumable  bool
}

func pathsForOutput(out string) checkpointPaths {
	return checkpointPaths{
		partial: out + ".partial",
		journal: out + ".resume.idx",
		state:   out + ".resume.json",
	}
}

func openArchiveWork(out string, opt Options, fingerprint, config string, header []byte) (*archiveWork, error) {
	w := &archiveWork{resumable: opt.Resume, paths: pathsForOutput(out)}
	if !opt.Resume {
		if exists(w.paths.partial) || exists(w.paths.journal) || exists(w.paths.state) {
			if !opt.Overwrite {
				if exists(w.paths.journal) && exists(w.paths.state) {
					return nil, fmt.Errorf("resumable partial exists; use -resume or -force to discard it")
				}
				return nil, fmt.Errorf("incomplete partial exists without a checkpoint; use -force to discard it")
			}
			if err := w.removeCheckpointFiles(); err != nil {
				return nil, err
			}
		}
		f, err := os.OpenFile(w.paths.partial, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0644)
		if err != nil {
			return nil, err
		}
		w.file, w.tempPath = f, w.paths.partial
		if _, err := f.Write(header); err != nil {
			w.Close(false)
			return nil, err
		}
		return w, nil
	}

	partialExists := exists(w.paths.partial)
	journalExists := exists(w.paths.journal)
	stateExists := exists(w.paths.state)
	anyExists := partialExists || journalExists || stateExists
	allExist := partialExists && journalExists && stateExists
	if anyExists && !allExist {
		if !opt.Overwrite {
			return nil, fmt.Errorf("incomplete resume checkpoint; use -force to discard it")
		}
		if err := w.removeCheckpointFiles(); err != nil {
			return nil, err
		}
		anyExists = false
	}
	var cp resumeCheckpoint
	if allExist {
		loaded, err := loadCheckpoint(w.paths.state)
		if err != nil {
			if !opt.Overwrite {
				return nil, err
			}
			if removeErr := w.removeCheckpointFiles(); removeErr != nil {
				return nil, removeErr
			}
			allExist = false
		} else {
			cp = loaded
		}
		if allExist && (cp.Fingerprint != fingerprint || cp.Config != config) {
			if !opt.Overwrite {
				if cp.Fingerprint != fingerprint {
					return nil, fmt.Errorf("source frames differ from checkpoint; use -force to start over")
				}
				return nil, fmt.Errorf("archive options differ from checkpoint; use -force to start over")
			}
			if err := w.removeCheckpointFiles(); err != nil {
				return nil, err
			}
			allExist = false
		}
	}
	if allExist {
		f, err := os.OpenFile(w.paths.partial, os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		j, err := os.OpenFile(w.paths.journal, os.O_RDWR, 0)
		if err != nil {
			f.Close()
			return nil, err
		}
		st, err := f.Stat()
		if err != nil || cp.DataOffset < int64(len(header)) || cp.DataOffset > st.Size() {
			f.Close()
			j.Close()
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("partial archive is shorter than its checkpoint")
		}
		gotHeader := make([]byte, len(header))
		if _, err := f.ReadAt(gotHeader, 0); err != nil {
			f.Close()
			j.Close()
			return nil, err
		}
		if string(gotHeader) != string(header) {
			f.Close()
			j.Close()
			return nil, fmt.Errorf("partial archive header does not match current input")
		}
		wantJournal := int64(cp.EntryCount) * IndexRecSize
		js, err := j.Stat()
		if err != nil || cp.EntryCount < 0 || wantJournal > js.Size() {
			f.Close()
			j.Close()
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("resume index is incomplete")
		}
		if err := f.Truncate(cp.DataOffset); err != nil {
			f.Close()
			j.Close()
			return nil, err
		}
		if err := j.Truncate(wantJournal); err != nil {
			f.Close()
			j.Close()
			return nil, err
		}
		entries, err := readEntryRecords(j, cp.EntryCount)
		if err != nil {
			f.Close()
			j.Close()
			return nil, err
		}
		if _, err := f.Seek(cp.DataOffset, io.SeekStart); err != nil {
			f.Close()
			j.Close()
			return nil, err
		}
		if _, err := j.Seek(0, io.SeekEnd); err != nil {
			f.Close()
			j.Close()
			return nil, err
		}
		w.file, w.journal, w.checkpoint, w.entries, w.start = f, j, cp, entries, cp.NextSegment
		return w, nil
	}

	f, err := os.OpenFile(w.paths.partial, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	j, err := os.OpenFile(w.paths.journal, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0644)
	if err != nil {
		f.Close()
		os.Remove(w.paths.partial)
		return nil, err
	}
	if _, err := f.Write(header); err != nil {
		f.Close()
		j.Close()
		w.removeCheckpointFiles()
		return nil, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		j.Close()
		w.removeCheckpointFiles()
		return nil, err
	}
	w.file, w.journal = f, j
	w.checkpoint = resumeCheckpoint{
		Version: Version, Fingerprint: fingerprint, Config: config,
		DataOffset: int64(len(header)),
	}
	if err := saveCheckpoint(w.paths.state, w.checkpoint); err != nil {
		w.Close(false)
		w.removeCheckpointFiles()
		return nil, err
	}
	return w, nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (w *archiveWork) CommitSegment(next int, entries []entry) error {
	if !w.resumable {
		return nil
	}
	if err := w.file.Sync(); err != nil {
		return err
	}
	if err := writeEntryRecords(w.journal, entries); err != nil {
		return err
	}
	if err := w.journal.Sync(); err != nil {
		return err
	}
	off, err := w.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	w.checkpoint.NextSegment = next
	w.checkpoint.EntryCount += len(entries)
	w.checkpoint.DataOffset = off
	return saveCheckpoint(w.paths.state, w.checkpoint)
}

func (w *archiveWork) Close(success bool) {
	if w.file != nil {
		w.file.Close()
	}
	if w.journal != nil {
		w.journal.Close()
	}
	if !success && !w.resumable && w.tempPath != "" {
		os.Remove(w.tempPath)
	}
}

func (w *archiveWork) RemoveCheckpointFiles() error {
	return w.removeCheckpointFiles()
}

func (w *archiveWork) removeCheckpointFiles() error {
	if w.paths.partial != "" {
		if err := os.Remove(w.paths.partial); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, p := range []string{w.paths.journal, w.paths.state} {
		if p != "" {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func checkpointConfig(opt Options, ffmpeg bool) string {
	return fmt.Sprintf("%d|%d|%d|%s|%s|%d|%t", Version, opt.GOPMax, opt.Tile, opt.Backend, opt.Preset, opt.FPS, ffmpeg)
}

func writeEntryRecords(w io.Writer, entries []entry) error {
	var rec [IndexRecSize]byte
	for _, e := range entries {
		clear(rec[:])
		binary.LittleEndian.PutUint64(rec[:8], e.offset)
		binary.LittleEndian.PutUint32(rec[8:], e.size)
		rec[12] = e.flags
		binary.LittleEndian.PutUint16(rec[13:], e.segPos)
		binary.LittleEndian.PutUint64(rec[15:], e.hash)
		if _, err := w.Write(rec[:]); err != nil {
			return err
		}
	}
	return nil
}

func readEntryRecords(r io.Reader, n int) ([]entry, error) {
	if n < 0 {
		return nil, fmt.Errorf("invalid checkpoint entry count: %d", n)
	}
	entries := make([]entry, n)
	var rec [IndexRecSize]byte
	for i := range entries {
		if _, err := io.ReadFull(r, rec[:]); err != nil {
			return nil, fmt.Errorf("resume index record %d: %w", i, err)
		}
		entries[i] = entry{
			offset: binary.LittleEndian.Uint64(rec[:8]),
			size:   binary.LittleEndian.Uint32(rec[8:]),
			flags:  rec[12],
			segPos: binary.LittleEndian.Uint16(rec[13:]),
			hash:   binary.LittleEndian.Uint64(rec[15:]),
		}
	}
	return entries, nil
}

func saveCheckpoint(path string, cp resumeCheckpoint) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".3dfc-checkpoint-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := json.NewEncoder(f).Encode(cp); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadCheckpoint(path string) (resumeCheckpoint, error) {
	f, err := os.Open(path)
	if err != nil {
		return resumeCheckpoint{}, err
	}
	defer f.Close()
	var cp resumeCheckpoint
	if err := json.NewDecoder(f).Decode(&cp); err != nil {
		return resumeCheckpoint{}, fmt.Errorf("read resume checkpoint: %w", err)
	}
	if cp.Version != Version {
		return resumeCheckpoint{}, fmt.Errorf("resume checkpoint format %d is not supported", cp.Version)
	}
	return cp, nil
}
