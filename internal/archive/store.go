package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"3dfc/internal/ff"
)

const (
	gopMin        = 8
	sceneCutRatio = 0.85
	motionRatio   = 0.45
)

type Options struct {
	GOPMax    int
	Tile      int
	Backend   string
	Preset    string
	FPS       int
	Overwrite bool
	Resume    bool
	Progress  func(stage string, done, total int)
	workDir   string
}

func DefaultOptions() Options {
	return Options{GOPMax: 300, Tile: 64, Backend: "auto", Preset: "max", FPS: 24}
}

type FrameFile struct {
	Path string
	Name string
	Size int64
}

func ListFramesInfo(dir string) ([]FrameFile, error) {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []FrameFile
	for _, e := range es {
		if e.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(e.Name()), ".png") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, FrameFile{
			Path: filepath.Join(dir, e.Name()),
			Name: e.Name(),
			Size: info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return naturalLess(out[i].Name, out[j].Name) })
	if len(out) == 0 {
		return nil, fmt.Errorf("no PNG files in %s", dir)
	}
	return out, nil
}

func ListFrames(dir string) ([]string, error) {
	ff, err := ListFramesInfo(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(ff))
	for i, f := range ff {
		out[i] = f.Path
	}
	return out, nil
}

type entry struct {
	offset uint64
	size   uint32
	flags  byte
	segPos uint16
	hash   uint64
}

type gopFrame struct {
	global     int
	name       string
	path       string
	fsize      int64
	sourceHash [32]byte
	hash       uint64
	ratio      float64
	alpha      bool
	pix        []byte
}

type segment struct {
	frames []gopFrame
	useFF  bool
	raw    bool
	tmpDir string
	w, h   int
}

const spillCap = 128 << 20

type locEntry struct {
	local uint64
	size  uint32
	flags byte
	pos   uint16
	hash  uint64
}

type gopResult struct {
	seg      int
	tmp      string
	data     []byte
	locs     []locEntry
	fallback bool
	err      error
}

const memCap = 256 << 20

func Create(indir, out string, opt Options) (*Report, error) {
	if !opt.Overwrite {
		if _, err := os.Lstat(out); err == nil {
			return nil, fmt.Errorf("output already exists: %s (use -force to replace it)", out)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	paths := pathsForOutput(out)
	partial := exists(paths.partial) || exists(paths.journal) || exists(paths.state)
	if partial && !opt.Resume && !opt.Overwrite {
		if exists(paths.journal) && exists(paths.state) {
			return nil, fmt.Errorf("resumable partial exists; use -resume to continue or -force to discard it")
		}
		return nil, fmt.Errorf("incomplete partial exists without a checkpoint; use -force to discard it")
	}
	if opt.Resume && !opt.Overwrite {
		count := 0
		for _, p := range []string{paths.partial, paths.journal, paths.state} {
			if exists(p) {
				count++
			}
		}
		if count > 0 && count < 3 {
			return nil, fmt.Errorf("incomplete resume checkpoint; use -force to discard it")
		}
	}
	files, err := ListFramesInfo(indir)
	if err != nil {
		return nil, err
	}
	ffOK := ff.Available()
	workDir := ""
	if opt.Resume {
		workDir = out + ".work"
		if err := os.RemoveAll(workDir); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(workDir, 0700); err != nil {
			return nil, err
		}
	} else {
		workDir, err = os.MkdirTemp("", "3dfc-work-*")
		if err != nil {
			return nil, err
		}
	}
	defer os.RemoveAll(workDir)
	opt.workDir = workDir
	tick := func(done int) {
		if opt.Progress != nil {
			opt.Progress("scan", done, len(files))
		}
	}
	segs, names, W, H, origSum, alphaAny, needFF, fingerprint, spills, err := scanFrames(files, opt, tick)
	if err != nil {
		for _, d := range spills {
			os.RemoveAll(d)
		}
		return nil, err
	}
	defer func() {
		for _, d := range spills {
			os.RemoveAll(d)
		}
	}()
	rep := &Report{Frames: len(files), Width: W, Height: H, Original: origSum}
	rep.Warnings = append(rep.Warnings, sequenceWarnings(files)...)
	if needFF && !ffOK && opt.Backend != "go" {
		rep.Warnings = append(rep.Warnings, "ffmpeg not found, Go engine used throughout")
	}
	hdr := makeArchiveHeader(W, H, len(files), origSum, opt, alphaAny)
	work, err := openArchiveWork(out, opt, fingerprint, checkpointConfig(opt, ffOK), hdr)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() { work.Close(committed) }()
	fw := work.file

	entries := work.entries
	if work.start < 0 || work.start > len(segs) {
		return nil, fmt.Errorf("resume checkpoint points past the end of the sequence")
	}
	completed := 0
	for i := 0; i < work.start; i++ {
		completed += len(segs[i].frames)
	}
	if len(entries) != completed {
		return nil, fmt.Errorf("resume index has %d frame entries, expected %d", len(entries), completed)
	}
	if opt.Progress != nil {
		opt.Progress("encode", completed, len(files))
	}
	var checkpointSegment func(next, entryStart int) error
	if opt.Resume {
		checkpointSegment = func(next, entryStart int) error {
			return work.CommitSegment(next, entries[entryStart:])
		}
	}
	if err := assemble(fw, segs, opt, ffOK, &entries, len(files), work.start, checkpointSegment); err != nil {
		return nil, err
	}

	namesOff, err := fw.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	if err := writeNames(fw, names); err != nil {
		return nil, err
	}
	indexOff, err := fw.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	if err := writeIndex(fw, entries); err != nil {
		return nil, err
	}
	var foot [FooterSize]byte
	binary.LittleEndian.PutUint64(foot[:8], uint64(namesOff))
	binary.LittleEndian.PutUint64(foot[8:], uint64(indexOff))
	if _, err := fw.Write(foot[:]); err != nil {
		return nil, err
	}
	if err := fw.Sync(); err != nil {
		return nil, err
	}
	st, err := fw.Stat()
	if err != nil {
		return nil, err
	}
	rep.Size = st.Size()
	if err := fw.Close(); err != nil {
		return nil, err
	}
	work.file = nil
	if work.journal != nil {
		if err := work.journal.Close(); err != nil {
			return nil, err
		}
		work.journal = nil
	}
	if !opt.Overwrite {
		if _, err := os.Lstat(out); err == nil {
			return nil, fmt.Errorf("output appeared while archiving: %s", out)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	commitPath := work.tempPath
	if work.resumable {
		commitPath = work.paths.partial
	}
	if err := os.Rename(commitPath, out); err != nil {
		return nil, fmt.Errorf("commit archive: %w", err)
	}
	committed = true
	if work.resumable {
		if err := work.RemoveCheckpointFiles(); err != nil {
			rep.Warnings = append(rep.Warnings, "archive committed but resume checkpoint cleanup failed: "+err.Error())
		}
	}
	return rep, nil
}

func makeArchiveHeader(w, h, count int, original int64, opt Options, hasAlpha bool) []byte {
	hdr := make([]byte, HeaderLen)
	copy(hdr, Magic)
	binary.LittleEndian.PutUint16(hdr[4:], Version)
	binary.LittleEndian.PutUint16(hdr[6:], uint16(opt.Tile))
	binary.LittleEndian.PutUint32(hdr[8:], uint32(w))
	binary.LittleEndian.PutUint32(hdr[12:], uint32(h))
	binary.LittleEndian.PutUint64(hdr[16:], uint64(count))
	binary.LittleEndian.PutUint32(hdr[24:], uint32(opt.GOPMax))
	if hasAlpha {
		hdr[28] = FlagAlpha
	}
	binary.LittleEndian.PutUint64(hdr[32:], uint64(original))
	return hdr
}

func scanFrames(files []FrameFile, opt Options, tick func(done int)) (segs []segment, names []string, W, H int, origSum int64, alphaAny, needFF bool, fingerprint string, spills []string, err error) {
	fail := func(e error) ([]segment, []string, int, int, int64, bool, bool, string, []string, error) {
		return nil, nil, 0, 0, 0, false, false, "", spills, e
	}
	W, H, err = pngDims(files[0].Path)
	if err != nil {
		return fail(fmt.Errorf("frame 0 unreadable: %w", err))
	}
	sourceHash := sha256.New()
	var prev []byte
	var cur []gopFrame
	var curBytes int64
	var curDir string
	spill := func() error {
		dir, err := os.MkdirTemp(opt.workDir, "spill-*")
		if err != nil {
			return err
		}
		for local, gf := range cur {
			img := &image.NRGBA{Pix: gf.pix, Stride: W * 4, Rect: image.Rect(0, 0, W, H)}
			if err := writeTempPNG(dir, local, img); err != nil {
				os.RemoveAll(dir)
				return err
			}
			cur[local].pix = nil
		}
		curDir = dir
		spills = append(spills, dir)
		curBytes = 0
		return nil
	}
	flush := func() {
		if len(cur) == 0 {
			return
		}
		useFF := decideBackend(opt, cur)
		if useFF && !ff.Available() {
			useFF = false
			needFF = true
		}
		segs = append(segs, segment{frames: cur, useFF: useFF, w: W, h: H, tmpDir: curDir})
		cur = nil
		curDir = ""
		curBytes = 0
	}
	for i, ff := range files {
		path := ff.Path
		name := ff.Name
		names = append(names, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return fail(err)
		}
		if int64(len(raw)) != ff.Size {
			return fail(fmt.Errorf("frame %d (%s) changed while scanning", i, name))
		}
		var nameLen [8]byte
		binary.LittleEndian.PutUint64(nameLen[:], uint64(len(name)))
		sourceHash.Write(nameLen[:])
		sourceHash.Write([]byte(name))
		fileHash := sha256.Sum256(raw)
		sourceHash.Write(fileHash[:])
		origSum += int64(len(raw))
		rgba, is16, err := decodeNRGBABytes(raw)
		if err != nil {
			return fail(fmt.Errorf("frame %d (%s): %w", i, name, err))
		}
		if rgba != nil && (rgba.Bounds().Dx() != W || rgba.Bounds().Dy() != H) {
			return fail(fmt.Errorf("frame %d (%s): size mismatch, sequence must be uniform", i, name))
		}
		if is16 {
			flush()
			segs = append(segs, segment{
				frames: []gopFrame{{global: i, name: name, path: path, fsize: ff.Size, sourceHash: fileHash}},
				raw:    true,
			})
			prev = nil
			tick(i + 1)
			continue
		}
		px := rgba.Pix
		a := hasAlpha(px)
		alphaAny = alphaAny || a
		h := hash64(px)
		gf := gopFrame{global: i, name: name, path: path, fsize: ff.Size, sourceHash: fileHash, hash: h, alpha: a, pix: px}
		if prev == nil {
			gf.ratio = 0
			cur = append(cur, gf)
			prev = append([]byte(nil), px...)
		} else {
			_, ratio := diffInfo(prev, px, W, H, opt.Tile)
			if len(cur) >= opt.GOPMax || (len(cur) >= gopMin && ratio > sceneCutRatio) {
				flush()
				gf.ratio = 0
				cur = append(cur, gf)
			} else {
				gf.ratio = ratio
				cur = append(cur, gf)
			}
			prev = append(prev[:0], px...)
		}
		curBytes += int64(len(px))
		if curDir == "" && curBytes > spillCap {
			if err := spill(); err != nil {
				return fail(err)
			}
		}
		tick(i + 1)
	}
	flush()
	return segs, names, W, H, origSum, alphaAny, needFF, hex.EncodeToString(sourceHash.Sum(nil)), spills, nil
}

func assemble(fw *os.File, segs []segment, opt Options, ffOK bool, entries *[]entry, total, startSeg int, checkpoint func(next, entryStart int) error) error {
	var done atomic.Int64
	for i := 0; i < startSeg; i++ {
		done.Add(int64(len(segs[i].frames)))
	}
	var mu sync.Mutex
	guarded := func() {
		if opt.Progress == nil {
			return
		}
		mu.Lock()
		opt.Progress("encode", int(done.Load()), total)
		mu.Unlock()
	}
	var jobs []int
	for si, s := range segs {
		if si >= startSeg && !s.raw {
			jobs = append(jobs, si)
		}
	}
	nw := runtime.NumCPU()
	if nw < 1 {
		nw = 1
	}
	if nw > 8 {
		nw = 8
	}
	jobCh := make(chan int, len(jobs))
	resCh := make(chan gopResult, len(jobs))
	ffSem := make(chan struct{}, 2)
	var wg sync.WaitGroup
	for w := 0; w < nw; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			enc, err := pickEncoder(opt.Preset)
			if err != nil {
				for si := range jobCh {
					resCh <- gopResult{seg: si, err: err}
				}
				return
			}
			defer enc.Close()
			for si := range jobCh {
				resCh <- encodeGOP(segs[si], si, enc, opt, ffOK, ffSem, &done, guarded)
			}
		}()
	}
	for _, si := range jobs {
		jobCh <- si
	}
	close(jobCh)
	defer wg.Wait()

	pending := map[int]gopResult{}
	cleanup := func() {
		for _, r := range pending {
			if r.tmp != "" {
				os.Remove(r.tmp)
			}
		}
	}
	commit := func(next, entryStart int) error {
		if checkpoint != nil {
			return checkpoint(next, entryStart)
		}
		return nil
	}
	for si := startSeg; si < len(segs); si++ {
		s := segs[si]
		entryStart := len(*entries)
		if s.raw {
			raw, err := os.ReadFile(s.frames[0].path)
			if err != nil {
				cleanup()
				return err
			}
			if sha256.Sum256(raw) != s.frames[0].sourceHash {
				cleanup()
				return fmt.Errorf("source frame changed during archive: %s", s.frames[0].name)
			}
			off, sz, err := writePacket(fw, PacketRaw, raw)
			if err != nil {
				cleanup()
				return err
			}
			*entries = append(*entries, entry{off, sz, FlagRaw, 0, hash64(raw)})
			done.Add(1)
			guarded()
			if err := commit(si+1, entryStart); err != nil {
				cleanup()
				return err
			}
			continue
		}
		var r gopResult
		for {
			if v, ok := pending[si]; ok {
				delete(pending, si)
				r = v
				break
			}
			v := <-resCh
			if v.seg == si {
				r = v
				break
			}
			pending[v.seg] = v
		}
		if r.err != nil {
			cleanup()
			return r.err
		}
		if r.fallback {
			for _, gf := range s.frames {
				raw, err := os.ReadFile(gf.path)
				if err != nil {
					cleanup()
					return err
				}
				if sha256.Sum256(raw) != gf.sourceHash {
					cleanup()
					return fmt.Errorf("source frame changed during archive: %s", gf.name)
				}
				off, sz, err := writePacket(fw, PacketRaw, raw)
				if err != nil {
					cleanup()
					return err
				}
				*entries = append(*entries, entry{off, sz, FlagRaw, 0, hash64(raw)})
			}
			if err := commit(si+1, entryStart); err != nil {
				cleanup()
				return err
			}
			continue
		}
		if r.data != nil {
			base, err := fw.Seek(0, io.SeekCurrent)
			if err != nil {
				cleanup()
				return err
			}
			if _, err := fw.Write(r.data); err != nil {
				cleanup()
				return err
			}
			for _, l := range r.locs {
				*entries = append(*entries, entry{uint64(base) + l.local, l.size, l.flags, l.pos, l.hash})
			}
			if err := commit(si+1, entryStart); err != nil {
				cleanup()
				return err
			}
			continue
		}
		base, err := fw.Seek(0, io.SeekCurrent)
		if err != nil {
			cleanup()
			return err
		}
		if err := copyFile(fw, r.tmp); err != nil {
			cleanup()
			os.Remove(r.tmp)
			return err
		}
		for _, l := range r.locs {
			*entries = append(*entries, entry{uint64(base) + l.local, l.size, l.flags, l.pos, l.hash})
		}
		os.Remove(r.tmp)
		if err := commit(si+1, entryStart); err != nil {
			cleanup()
			return err
		}
	}
	return nil
}

func copyFile(fw *os.File, tmp string) error {
	f, err := os.Open(tmp)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(fw, f)
	return err
}

func encodeGOP(seg segment, si int, enc zstdEncoder, opt Options, ffOK bool, ffSem chan struct{}, done *atomic.Int64, guarded func()) gopResult {
	var est int64
	for _, gf := range seg.frames {
		if gf.pix != nil {
			est += int64(len(gf.pix))
		} else {
			est += gf.fsize
		}
	}
	mem := est <= memCap
	var mbuf []byte
	var tmp *os.File
	var tmpName string
	var err error
	if !mem {
		tmp, err = os.CreateTemp(opt.workDir, "packets-*")
		if err != nil {
			return gopResult{seg: si, err: err}
		}
		tmpName = tmp.Name()
	}
	fail := func(err error) gopResult {
		if tmp != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
		return gopResult{seg: si, err: err}
	}
	emit := func(typ byte, payload []byte) (uint64, error) {
		if mem {
			off := uint64(len(mbuf))
			mbuf = append(mbuf, typ)
			mbuf = binary.LittleEndian.AppendUint32(mbuf, uint32(len(payload)))
			mbuf = append(mbuf, payload...)
			return off, nil
		}
		off, err := tmp.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, err
		}
		var hh [5]byte
		hh[0] = typ
		binary.LittleEndian.PutUint32(hh[1:], uint32(len(payload)))
		if _, err := tmp.Write(hh[:]); err != nil {
			return 0, err
		}
		if _, err := tmp.Write(payload); err != nil {
			return 0, err
		}
		return uint64(off), nil
	}
	var locs []locEntry
	if seg.useFF && ffOK {
		ffSem <- struct{}{}
		body, err := encodeFFBody(seg, opt)
		<-ffSem
		if err != nil {
			return fail(err)
		}
		off, err := emit(PacketFF, body)
		if err != nil {
			return fail(err)
		}
		for i, gf := range seg.frames {
			flags := byte(FlagFF)
			if i == 0 {
				flags |= FlagKey
			}
			locs = append(locs, locEntry{off, uint32(5 + len(body)), flags, uint16(i), gf.hash})
		}
	} else {
		var prev []byte
		getPix := func(local int, gf gopFrame) ([]byte, error) {
			if gf.pix != nil {
				return gf.pix, nil
			}
			rgba, _, err := decodeNRGBA(filepath.Join(seg.tmpDir, fmt.Sprintf("gop_%06d.png", local)))
			if err != nil {
				return nil, err
			}
			return rgba.Pix, nil
		}
		for local, gf := range seg.frames {
			cur, err := getPix(local, gf)
			if err != nil {
				return fail(err)
			}
			var typ byte
			var payload []byte
			if local == 0 {
				typ = PacketGoI
				payload = enc.EncodeAll(cur, nil)
			} else {
				typ = PacketGoP
				changed, _ := diffInfo(prev, cur, seg.w, seg.h, opt.Tile)
				payload = encodeGoP(enc, prev, cur, seg.w, seg.h, opt.Tile, changed)
			}
			off, err := emit(typ, payload)
			if err != nil {
				return fail(err)
			}
			flags := byte(0)
			if local == 0 {
				flags |= FlagKey
			}
			locs = append(locs, locEntry{uint64(off), uint32(5 + len(payload)), flags, 0, gf.hash})
			prev = append(prev[:0], cur...)
			done.Add(1)
			guarded()
		}
	}
	var total int64
	if mem {
		total = int64(len(mbuf))
	} else {
		if err := tmp.Close(); err != nil {
			os.Remove(tmpName)
			return gopResult{seg: si, err: err}
		}
		st, err := os.Stat(tmpName)
		if err != nil {
			os.Remove(tmpName)
			return gopResult{seg: si, err: err}
		}
		total = st.Size()
	}
	var origSum int64
	for _, gf := range seg.frames {
		origSum += gf.fsize
	}
	if total > origSum+5*int64(len(seg.frames)) {
		if tmp != nil {
			os.Remove(tmpName)
		}
		if seg.useFF && ffOK {
			done.Add(int64(len(seg.frames)))
			guarded()
		}
		return gopResult{seg: si, fallback: true}
	}
	if seg.useFF && ffOK {
		done.Add(int64(len(seg.frames)))
		guarded()
	}
	if mem {
		return gopResult{seg: si, data: mbuf, locs: locs}
	}
	return gopResult{seg: si, tmp: tmpName, locs: locs}
}

func encodeFFBody(seg segment, opt Options) ([]byte, error) {
	dir := seg.tmpDir
	if dir == "" {
		var err error
		dir, err = os.MkdirTemp(opt.workDir, "gop-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
	}
	for local, gf := range seg.frames {
		if gf.pix == nil {
			continue
		}
		img := &image.NRGBA{Pix: gf.pix, Stride: seg.w * 4, Rect: image.Rect(0, 0, seg.w, seg.h)}
		if err := writeTempPNG(dir, local, img); err != nil {
			return nil, err
		}
	}
	withAlpha := false
	for _, gf := range seg.frames {
		withAlpha = withAlpha || gf.alpha
	}
	rgb, alpha, err := ff.EncodeGOP(dir, len(seg.frames), opt.FPS, opt.Preset, withAlpha)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(len(seg.frames)))
	binary.Write(&buf, binary.LittleEndian, uint32(len(rgb)))
	binary.Write(&buf, binary.LittleEndian, uint32(len(alpha)))
	buf.Write(rgb)
	buf.Write(alpha)
	return buf.Bytes(), nil
}

func writeTempPNG(dir string, local int, img *image.NRGBA) error {
	f, err := os.Create(filepath.Join(dir, fmt.Sprintf("gop_%06d.png", local)))
	if err != nil {
		return err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	err = enc.Encode(f, img)
	f.Close()
	return err
}

func writePacket(fw *os.File, typ byte, payload []byte) (uint64, uint32, error) {
	off, err := fw.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, 0, err
	}
	var h [5]byte
	h[0] = typ
	binary.LittleEndian.PutUint32(h[1:], uint32(len(payload)))
	if _, err := fw.Write(h[:]); err != nil {
		return 0, 0, err
	}
	if _, err := fw.Write(payload); err != nil {
		return 0, 0, err
	}
	return uint64(off), uint32(5 + len(payload)), nil
}

func decideBackend(opt Options, gop []gopFrame) bool {
	if len(gop) < 4 {
		return false
	}
	switch opt.Backend {
	case "ffmpeg":
		return true
	case "go":
		return false
	}
	sum := 0.0
	for _, f := range gop {
		sum += f.ratio
	}
	return sum/float64(len(gop)) > motionRatio
}

func writeRawGOP(fw *os.File, gop []gopFrame, entries *[]entry) error {
	for _, gf := range gop {
		raw, err := os.ReadFile(gf.path)
		if err != nil {
			return err
		}
		if sha256.Sum256(raw) != gf.sourceHash {
			return fmt.Errorf("source frame changed during archive: %s", gf.name)
		}
		off, sz, err := writePacket(fw, PacketRaw, raw)
		if err != nil {
			return err
		}
		*entries = append(*entries, entry{off, sz, FlagRaw, 0, hash64(raw)})
	}
	return nil
}

func writeNames(fw *os.File, names []string) error {
	binary.Write(fw, binary.LittleEndian, uint64(len(names)))
	for _, n := range names {
		if len(n) > 0xffff {
			return fmt.Errorf("frame name too long (%d bytes): %q", len(n), n[:32])
		}
		binary.Write(fw, binary.LittleEndian, uint16(len(n)))
		io.WriteString(fw, n)
	}
	return nil
}

func writeIndex(fw *os.File, entries []entry) error {
	binary.Write(fw, binary.LittleEndian, uint64(len(entries)))
	var rec [IndexRecSize]byte
	for _, e := range entries {
		binary.LittleEndian.PutUint64(rec[:8], e.offset)
		binary.LittleEndian.PutUint32(rec[8:], e.size)
		rec[12] = e.flags
		binary.LittleEndian.PutUint16(rec[13:], e.segPos)
		binary.LittleEndian.PutUint64(rec[15:], e.hash)
		if _, err := fw.Write(rec[:]); err != nil {
			return err
		}
	}
	return nil
}

func pngDims(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	c, err := png.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return c.Width, c.Height, nil
}

func toNRGBA(img image.Image) *image.NRGBA {
	if r, ok := img.(*image.NRGBA); ok {
		return r
	}
	b := img.Bounds()
	r := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	if src, ok := img.(*image.RGBA); ok && b.Min == (image.Point{}) {
		fastRGBA2NRGBA(r.Pix, src.Pix)
		return r
	}
	draw.Draw(r, r.Bounds(), img, b.Min, draw.Src)
	return r
}
