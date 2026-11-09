package archive

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"3dfc/internal/ff"
	"github.com/klauspost/compress/zstd"
)

const parallelMin = 64

type Archive struct {
	path    string
	w, h    int
	tile    int
	count   int
	gopMax  uint32
	flags   byte
	orig    int64
	names   []string
	entries []entry
}

func Open(path string) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < int64(HeaderLen+FooterSize) {
		return nil, fmt.Errorf("file too small, not a .3dfc archive")
	}
	pre := make([]byte, 6)
	if _, err := io.ReadFull(f, pre); err != nil {
		return nil, err
	}
	if string(pre[:4]) != Magic {
		return nil, fmt.Errorf("bad magic: not a 3dfc archive")
	}
	ver := binary.LittleEndian.Uint16(pre[4:])
	if ver != Version {
		return nil, fmt.Errorf("version %d unsupported (tool v%d)", ver, Version)
	}
	hdr := make([]byte, HeaderLen-6)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return nil, err
	}
	a := &Archive{path: path}
	a.tile = int(binary.LittleEndian.Uint16(hdr[0:]))
	a.w = int(binary.LittleEndian.Uint32(hdr[2:]))
	a.h = int(binary.LittleEndian.Uint32(hdr[6:]))
	a.count = int(binary.LittleEndian.Uint64(hdr[10:]))
	a.gopMax = binary.LittleEndian.Uint32(hdr[18:])
	a.flags = hdr[22]
	a.orig = int64(binary.LittleEndian.Uint64(hdr[26:]))

	foot := make([]byte, FooterSize)
	if _, err := f.Seek(-FooterSize, io.SeekEnd); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(f, foot); err != nil {
		return nil, err
	}
	namesOff := int64(binary.LittleEndian.Uint64(foot[:8]))
	indexOff := int64(binary.LittleEndian.Uint64(foot[8:]))

	if _, err := f.Seek(namesOff, io.SeekStart); err != nil {
		return nil, err
	}
	var n uint64
	if err := binary.Read(f, binary.LittleEndian, &n); err != nil {
		return nil, err
	}
	a.names = make([]string, n)
	for i := range a.names {
		var l uint16
		if err := binary.Read(f, binary.LittleEndian, &l); err != nil {
			return nil, err
		}
		b := make([]byte, l)
		if _, err := io.ReadFull(f, b); err != nil {
			return nil, err
		}
		a.names[i] = string(b)
	}
	if _, err := f.Seek(indexOff, io.SeekStart); err != nil {
		return nil, err
	}
	if err := binary.Read(f, binary.LittleEndian, &n); err != nil {
		return nil, err
	}
	if int(n) != a.count {
		return nil, fmt.Errorf("corrupt index: %d vs %d frames", n, a.count)
	}
	a.entries = make([]entry, n)
	rec := make([]byte, IndexRecSize)
	for i := range a.entries {
		if _, err := io.ReadFull(f, rec); err != nil {
			return nil, err
		}
		a.entries[i] = entry{
			offset: binary.LittleEndian.Uint64(rec[:8]),
			size:   binary.LittleEndian.Uint32(rec[8:]),
			flags:  rec[12],
			segPos: binary.LittleEndian.Uint16(rec[13:]),
			hash:   binary.LittleEndian.Uint64(rec[15:]),
		}
	}
	return a, nil
}

func (a *Archive) Count() int { return a.count }

func (a *Archive) CheckRange(lo, hi int) error {
	if lo < 0 || hi < 0 || lo >= a.count || hi >= a.count {
		return fmt.Errorf("range [%d..%d] outside %d frames (0..%d)", lo, hi, a.count, a.count-1)
	}
	if lo > hi {
		return fmt.Errorf("inverted range: from %d > to %d", lo, hi)
	}
	return nil
}

type unitKind int

const (
	uGo unitKind = iota
	uFF
	uRaw
)

type unit struct {
	kind           unitKind
	decStart       int
	decEnd         int
	emitLo, emitHi int
}

func (a *Archive) continues(k int) bool {
	e := a.entries[k]
	if e.flags&(FlagRaw|FlagKey) != 0 {
		return false
	}
	if e.flags&FlagFF != 0 {
		return a.entries[k-1].offset == e.offset
	}
	return true
}

func (a *Archive) plan(lo, hi int) []unit {
	s := lo
	for s > 0 && a.continues(s) {
		s--
	}
	var us []unit
	i := s
	for i <= hi {
		e := a.entries[i]
		switch {
		case e.flags&FlagRaw != 0:
			us = append(us, unit{uRaw, i, i, i, i})
			i++
		case e.flags&FlagFF != 0:
			g := i
			for g+1 < a.count && a.entries[g+1].offset == e.offset {
				g++
			}
			us = append(us, unit{uFF, i, g, max(i, lo), min(g, hi)})
			i = g + 1
		default:
			j := i
			for j+1 < a.count && a.entries[j+1].flags == 0 {
				j++
			}
			end := min(j, hi)
			us = append(us, unit{uGo, i, end, max(i, lo), end})
			i = j + 1
		}
	}
	return us
}

func readPacket(f *os.File, e entry) (byte, []byte, error) {
	if _, err := f.Seek(int64(e.offset), io.SeekStart); err != nil {
		return 0, nil, err
	}
	var h [5]byte
	if _, err := io.ReadFull(f, h[:]); err != nil {
		return 0, nil, err
	}
	n := binary.LittleEndian.Uint32(h[1:])
	if n > 1<<31-1 {
		return 0, nil, fmt.Errorf("%d-byte packet: corrupt archive?", n)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(f, payload); err != nil {
		return 0, nil, err
	}
	return h[0], payload, nil
}

func parseFF(payload []byte) (rgb, alpha []byte, segFrames int, err error) {
	if len(payload) < 12 {
		return nil, nil, 0, fmt.Errorf("truncated ffmpeg segment")
	}
	segFrames = int(binary.LittleEndian.Uint32(payload[:4]))
	rgbLen := int(binary.LittleEndian.Uint32(payload[4:8]))
	alphaLen := int(binary.LittleEndian.Uint32(payload[8:12]))
	rgb = payload[12:]
	if len(rgb) < rgbLen {
		return nil, nil, 0, fmt.Errorf("truncated rgb segment")
	}
	if alphaLen > 0 {
		alpha = rgb[rgbLen:]
		if len(alpha) < alphaLen {
			return nil, nil, 0, fmt.Errorf("truncated alpha segment")
		}
		alpha = alpha[:alphaLen]
	}
	return rgb[:rgbLen], alpha, segFrames, nil
}

func writeNRGBAAtomic(path string, px []byte, w, h int, overwrite bool) error {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	copy(img.Pix, px)
	return writeFrameAtomic(path, overwrite, func(f *os.File) error {
		enc := png.Encoder{CompressionLevel: png.BestSpeed}
		return enc.Encode(f, img)
	})
}

type ExtractOptions struct {
	Overwrite bool
}

func (a *Archive) ExtractRange(lo, hi int, outDir string, prog func(done, total int)) error {
	return a.ExtractRangeWithOptions(lo, hi, outDir, ExtractOptions{}, prog)
}

func (a *Archive) ExtractRangeWithOptions(lo, hi int, outDir string, opt ExtractOptions, prog func(done, total int)) error {
	if err := a.CheckRange(lo, hi); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	if err := a.checkOutputFiles(lo, hi, outDir, opt.Overwrite); err != nil {
		return err
	}
	units := a.plan(lo, hi)
	total := hi - lo + 1
	if total < parallelMin || runtime.NumCPU() < 2 {
		return a.extractSeq(units, outDir, opt.Overwrite, prog, total)
	}
	return a.extractPar(units, outDir, opt.Overwrite, prog, total)
}

func (a *Archive) checkOutputFiles(lo, hi int, outDir string, overwrite bool) error {
	seen := make(map[string]string, hi-lo+1)
	for i := lo; i <= hi; i++ {
		name := a.names[i]
		if name == "" || filepath.IsAbs(name) || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
			return fmt.Errorf("unsafe frame name in archive: %q", name)
		}
		path := filepath.Join(outDir, name)
		key := strings.ToLower(filepath.Clean(path))
		if prev, ok := seen[key]; ok {
			return fmt.Errorf("frame names %q and %q collide on this filesystem", prev, name)
		}
		seen[key] = name
		if !overwrite {
			if _, err := os.Lstat(path); err == nil {
				return fmt.Errorf("output file already exists: %s (use -overwrite to replace it)", path)
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func writeFrameAtomic(path string, overwrite bool, write func(*os.File) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".3dfc-frame-*.partial")
	if err != nil {
		return err
	}
	tmp := f.Name()
	committed := false
	defer func() {
		f.Close()
		if !committed {
			os.Remove(tmp)
		}
	}()
	if err := write(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if !overwrite {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("output file appeared while restoring: %s", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	committed = true
	return nil
}

func (a *Archive) extractSeq(units []unit, outDir string, overwrite bool, prog func(done, total int), total int) error {
	f, err := os.Open(a.path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec, err := newZstdDecoder()
	if err != nil {
		return err
	}
	defer dec.Close()
	n := 0
	emit := func() {
		n++
		if prog != nil {
			prog(n, total)
		}
	}
	for _, u := range units {
		if err := a.execUnit(f, dec, u, outDir, overwrite, emit); err != nil {
			return err
		}
	}
	return nil
}

func (a *Archive) extractPar(units []unit, outDir string, overwrite bool, prog func(done, total int), total int) error {
	var done atomic.Int64
	var mu sync.Mutex
	emit := func() {
		n := int(done.Add(1))
		if prog == nil {
			return
		}
		mu.Lock()
		prog(n, total)
		mu.Unlock()
	}
	jobs := make(chan unit, len(units))
	errCh := make(chan error, runtime.NumCPU())
	var stop atomic.Bool
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := os.Open(a.path)
			if err != nil {
				errCh <- err
				stop.Store(true)
				return
			}
			defer f.Close()
			dec, err := newZstdDecoder()
			if err != nil {
				errCh <- err
				stop.Store(true)
				return
			}
			defer dec.Close()
			for u := range jobs {
				if stop.Load() {
					continue
				}
				if err := a.execUnit(f, dec, u, outDir, overwrite, emit); err != nil {
					errCh <- err
					stop.Store(true)
					return
				}
			}
		}()
	}
	for _, u := range units {
		jobs <- u
	}
	close(jobs)
	wg.Wait()
	close(errCh)
	for e := range errCh {
		return e
	}
	return nil
}

func (a *Archive) execUnit(f *os.File, dec *zstd.Decoder, u unit, outDir string, overwrite bool, emit func()) error {
	switch u.kind {
	case uRaw:
		_, payload, err := readPacket(f, a.entries[u.decStart])
		if err != nil {
			return err
		}
		if err := writeFrameAtomic(filepath.Join(outDir, a.names[u.decStart]), overwrite, func(f *os.File) error {
			_, err := f.Write(payload)
			return err
		}); err != nil {
			return err
		}
		emit()
		return nil
	case uGo:
		var prev []byte
		for k := u.decStart; k <= u.decEnd; k++ {
			typ, payload, err := readPacket(f, a.entries[k])
			if err != nil {
				return err
			}
			var cur []byte
			switch typ {
			case PacketGoI:
				cur, err = dec.DecodeAll(payload, nil)
				if err != nil {
					return fmt.Errorf("frame %d: %w", k, err)
				}
			case PacketGoP:
				if prev == nil {
					return fmt.Errorf("frame %d: broken P chain (corrupt archive?)", k)
				}
				cur, err = decodeGoP(dec, prev, a.w, a.h, a.tile, payload)
				if err != nil {
					return fmt.Errorf("frame %d: %w", k, err)
				}
			default:
				return fmt.Errorf("frame %d: unexpected packet in Go run", k)
			}
			prev = cur
			if k >= u.emitLo && k <= u.emitHi {
				if err := writeNRGBAAtomic(filepath.Join(outDir, a.names[k]), cur, a.w, a.h, overwrite); err != nil {
					return err
				}
				emit()
			}
		}
		return nil
	default:
		_, payload, err := readPacket(f, a.entries[u.decStart])
		if err != nil {
			return err
		}
		rgb, alpha, segFrames, err := parseFF(payload)
		if err != nil {
			return err
		}
		if !ff.Available() {
			return fmt.Errorf("ffmpeg is required to extract these frames (not in PATH)")
		}
		work, err := os.MkdirTemp("", "3dfc-dec-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)
		if err := ff.DecodeGOP(work, rgb, alpha, segFrames); err != nil {
			return err
		}
		gopStart := u.decStart - int(a.entries[u.decStart].segPos)
		for k := u.emitLo; k <= u.emitHi; k++ {
			px, err := combineFFFrame(work, k-gopStart, len(alpha) > 0)
			if err != nil {
				return fmt.Errorf("frame %d: %w", k, err)
			}
			if err := writeNRGBAAtomic(filepath.Join(outDir, a.names[k]), px, a.w, a.h, overwrite); err != nil {
				return err
			}
			emit()
		}
		return nil
	}
}

func combineFFFrame(work string, local int, withAlpha bool) ([]byte, error) {
	f, err := os.Open(filepath.Join(work, fmt.Sprintf("out_%06d.png", local+1)))
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return nil, err
	}
	rgb := toNRGBA(img)
	if !withAlpha {
		return rgb.Pix, nil
	}
	fa, err := os.Open(filepath.Join(work, fmt.Sprintf("aout_%06d.png", local+1)))
	if err != nil {
		return nil, err
	}
	ag, _, err := image.Decode(fa)
	fa.Close()
	if err != nil {
		return nil, err
	}
	gray := toGray(ag)
	for i := 0; i < len(rgb.Pix)/4; i++ {
		rgb.Pix[i*4+3] = gray.Pix[i]
	}
	return rgb.Pix, nil
}
