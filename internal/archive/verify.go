// Copyright (C) 2026 Vinetwigs
// SPDX-License-Identifier: GPL-3.0-only

package archive

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"runtime"
	"sync"
	"sync/atomic"

	"3dfc/internal/ff"
	"github.com/klauspost/compress/zstd"
)

func (a *Archive) Summary() []string {
	var nGo, nFF, nRaw, nKey int
	for _, e := range a.entries {
		switch {
		case e.flags&FlagFF != 0:
			nFF++
		case e.flags&FlagRaw != 0:
			nRaw++
		default:
			nGo++
		}
		if e.flags&FlagKey != 0 {
			nKey++
		}
	}
	st, _ := os.Stat(a.path)
	var size int64
	if st != nil {
		size = st.Size()
	}
	out := []string{
		fmt.Sprintf("frames: %d (%dx%d, tile %d)", a.count, a.w, a.h, a.tile),
		fmt.Sprintf("alpha: %s", map[bool]string{true: "yes", false: "no"}[a.flags&FlagAlpha != 0]),
	}
	out = append(out, SavingsLines(a.orig, size)...)
	out = append(out,
		fmt.Sprintf("engine: go %d · ffmpeg %d · passthrough %d · keyframes %d", nGo, nFF, nRaw, nKey),
		fmt.Sprintf("range: 0..%d", a.count-1),
	)
	return out
}

func (a *Archive) Verify(prog func(done, total int)) error {
	total := a.count
	units := a.plan(0, total-1)
	if total < parallelMin || runtime.NumCPU() < 2 {
		return a.verifySeq(units, prog, total)
	}
	return a.verifyPar(units, prog, total)
}

func (a *Archive) verifySeq(units []unit, prog func(done, total int), total int) error {
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
	bad := 0
	for _, u := range units {
		m, err := a.execVerify(f, dec, u)
		if err != nil {
			return err
		}
		bad += m
		for k := u.emitLo; k <= u.emitHi; k++ {
			n++
			if prog != nil {
				prog(n, total)
			}
		}
	}
	if bad > 0 {
		return fmt.Errorf("verify failed: %d corrupt frames", bad)
	}
	return nil
}

func (a *Archive) verifyPar(units []unit, prog func(done, total int), total int) error {
	var done atomic.Int64
	var bad atomic.Int64
	var mu sync.Mutex
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
				m, err := a.execVerify(f, dec, u)
				if err != nil {
					errCh <- err
					stop.Store(true)
					return
				}
				if m > 0 {
					bad.Add(int64(m))
				}
				n := int(done.Add(int64(u.emitHi - u.emitLo + 1)))
				if prog != nil {
					mu.Lock()
					prog(n, total)
					mu.Unlock()
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
	if bad.Load() > 0 {
		return fmt.Errorf("verify failed: %d corrupt frames", bad.Load())
	}
	return nil
}

func (a *Archive) execVerify(f *os.File, dec *zstd.Decoder, u unit) (int, error) {
	switch u.kind {
	case uRaw:
		_, payload, err := readPacket(f, a.entries[u.decStart])
		if err != nil {
			return 0, err
		}
		if hash64(payload) != a.entries[u.decStart].hash {
			return 1, nil
		}
		return 0, nil
	case uGo:
		var prev []byte
		bad := 0
		for k := u.decStart; k <= u.decEnd; k++ {
			typ, payload, err := readPacket(f, a.entries[k])
			if err != nil {
				return 0, err
			}
			var cur []byte
			switch typ {
			case PacketGoI:
				cur, err = dec.DecodeAll(payload, nil)
			case PacketGoP:
				if prev == nil {
					return 0, fmt.Errorf("frame %d: broken P chain", k)
				}
				cur, err = decodeGoP(dec, prev, a.w, a.h, a.tile, payload)
			default:
				return 0, fmt.Errorf("frame %d: unexpected type %d", k, typ)
			}
			if err != nil {
				return 0, fmt.Errorf("frame %d: %w", k, err)
			}
			prev = cur
			if k >= u.emitLo && k <= u.emitHi && hash64(cur) != a.entries[k].hash {
				bad++
			}
		}
		return bad, nil
	default:
		_, payload, err := readPacket(f, a.entries[u.decStart])
		if err != nil {
			return 0, err
		}
		rgb, alpha, segFrames, err := parseFF(payload)
		if err != nil {
			return 0, err
		}
		if !ff.Available() {
			return 0, fmt.Errorf("ffmpeg is required to verify these segments")
		}
		work, err := os.MkdirTemp("", "3dfc-vfy-*")
		if err != nil {
			return 0, err
		}
		defer os.RemoveAll(work)
		if err := ff.DecodeGOP(work, rgb, alpha, segFrames); err != nil {
			return 0, err
		}
		gopStart := u.decStart - int(a.entries[u.decStart].segPos)
		bad := 0
		for k := u.emitLo; k <= u.emitHi; k++ {
			px, err := combineFFFrame(work, k-gopStart, len(alpha) > 0)
			if err != nil {
				return 0, err
			}
			if hash64(px) != a.entries[k].hash {
				bad++
			}
		}
		return bad, nil
	}
}

func toGray(img image.Image) *image.Gray {
	if g, ok := img.(*image.Gray); ok {
		return g
	}
	b := img.Bounds()
	g := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, _, _, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			g.SetGray(x, y, color.Gray{Y: uint8(r >> 8)})
		}
	}
	return g
}
