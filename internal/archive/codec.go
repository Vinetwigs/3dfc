package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	_ "image/png"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

func hash64(b []byte) uint64 {
	s := sha256.Sum256(b)
	return binary.LittleEndian.Uint64(s[:8])
}

func decodeNRGBA(path string) (*image.NRGBA, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	return decodeNRGBAReader(f)
}

func decodeNRGBABytes(data []byte) (*image.NRGBA, bool, error) {
	return decodeNRGBAReader(bytes.NewReader(data))
}

func decodeNRGBAReader(src io.Reader) (*image.NRGBA, bool, error) {
	img, _, err := image.Decode(src)
	if err != nil {
		return nil, false, err
	}
	switch img.(type) {
	case *image.RGBA64, *image.NRGBA64, *image.Gray16:
		return nil, true, nil
	}
	b := img.Bounds()
	if n, ok := img.(*image.NRGBA); ok && b.Min == (image.Point{}) {
		return n, false, nil
	}
	r := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	if src, ok := img.(*image.RGBA); ok && b.Min == (image.Point{}) {
		fastRGBA2NRGBA(r.Pix, src.Pix)
		return r, false, nil
	}
	draw.Draw(r, r.Bounds(), img, b.Min, draw.Src)
	return r, false, nil
}

func fastRGBA2NRGBA(dst, src []byte) {
	opaque := true
	for i := 3; i < len(src); i += 4 {
		if src[i] != 255 {
			opaque = false
			break
		}
	}
	if opaque {
		copy(dst, src)
		return
	}
	for i := 0; i < len(src); i += 4 {
		a8 := src[i+3]
		if a8 == 0 {
			dst[i], dst[i+1], dst[i+2], dst[i+3] = 0, 0, 0, 0
			continue
		}
		if a8 == 0xff {
			dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i], src[i+1], src[i+2], 0xff
			continue
		}
		a := uint32(a8) * 0x101
		r := uint32(src[i]) * 0x101
		g := uint32(src[i+1]) * 0x101
		b := uint32(src[i+2]) * 0x101
		dst[i] = uint8(((r * 0xffff) / a) >> 8)
		dst[i+1] = uint8(((g * 0xffff) / a) >> 8)
		dst[i+2] = uint8(((b * 0xffff) / a) >> 8)
		dst[i+3] = a8
	}
}

func hasAlpha(px []byte) bool {
	for i := 3; i < len(px); i += 4 {
		if px[i] != 255 {
			return true
		}
	}
	return false
}

func tilesXY(w, h, tile int) (nx, ny int) {
	return (w + tile - 1) / tile, (h + tile - 1) / tile
}

func tileRect(w, h, tile, idx int) (x, y, tw, th int) {
	nx, _ := tilesXY(w, h, tile)
	tx, ty := idx%nx, idx/nx
	x, y = tx*tile, ty*tile
	tw, th = tile, tile
	if x+tw > w {
		tw = w - x
	}
	if y+th > h {
		th = h - y
	}
	return
}

func tileEqual(prev, cur []byte, w, x, y, tw, th int) bool {
	stride := w * 4
	for r := 0; r < th; r++ {
		off := (y+r)*stride + x*4
		if !bytes.Equal(prev[off:off+tw*4], cur[off:off+tw*4]) {
			return false
		}
	}
	return true
}

func diffInfo(prev, cur []byte, w, h, tile int) (changed []uint32, ratio float64) {
	nx, ny := tilesXY(w, h, tile)
	total := nx * ny
	for i := 0; i < total; i++ {
		x, y, tw, th := tileRect(w, h, tile, i)
		if !tileEqual(prev, cur, w, x, y, tw, th) {
			changed = append(changed, uint32(i))
		}
	}
	return changed, float64(len(changed)) / float64(total)
}

func newZstdEncoder() (*zstd.Encoder, error) {
	return zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
}

type zstdEncoder interface {
	zenc
	Close() error
}

type zenc interface {
	EncodeAll(src, dst []byte) []byte
}

type zdec interface {
	DecodeAll(src, dst []byte) ([]byte, error)
}

func pickEncoder(preset string) (zstdEncoder, error) {
	if preset == "fast" {
		return zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	}
	return newZstdEncoder()
}

func newZstdDecoder() (*zstd.Decoder, error) {
	return zstd.NewReader(nil)
}

func encodeGoP(enc zenc, prev, cur []byte, w, h, tile int, changed []uint32) []byte {
	type rect struct{ x, y, tw, th int }
	rects := make([]rect, len(changed))
	total := 0
	for k, i := range changed {
		x, y, tw, th := tileRect(w, h, tile, int(i))
		rects[k] = rect{x, y, tw, th}
		total += tw * th * 4
	}
	raw := make([]byte, 0, 4+4*len(changed)+total)
	raw = binary.LittleEndian.AppendUint32(raw, uint32(len(changed)))
	for _, i := range changed {
		raw = binary.LittleEndian.AppendUint32(raw, i)
	}
	stride := w * 4
	for _, r := range rects {
		for y := 0; y < r.th; y++ {
			off := (r.y+y)*stride + r.x*4
			for c := 0; c < r.tw*4; c++ {
				raw = append(raw, cur[off+c]-prev[off+c])
			}
		}
	}
	return enc.EncodeAll(raw, nil)
}

func decodeGoP(dec zdec, prev []byte, w, h, tile int, payload []byte) ([]byte, error) {
	raw, err := dec.DecodeAll(payload, nil)
	if err != nil {
		return nil, fmt.Errorf("zstd P: %w", err)
	}
	if len(raw) < 4 {
		return nil, fmt.Errorf("truncated P packet")
	}
	n := binary.LittleEndian.Uint32(raw[:4])
	raw = raw[4:]
	if len(raw) < int(n)*4 {
		return nil, fmt.Errorf("truncated tile list")
	}
	cur := append([]byte(nil), prev...)
	stride := w * 4
	pos := int(n) * 4
	for k := 0; k < int(n); k++ {
		idx := binary.LittleEndian.Uint32(raw[k*4:])
		x, y, tw, th := tileRect(w, h, tile, int(idx))
		for r := 0; r < th; r++ {
			off := (y+r)*stride + x*4
			for c := 0; c < tw*4; c++ {
				if pos >= len(raw) {
					return nil, fmt.Errorf("truncated delta")
				}
				cur[off+c] = prev[off+c] + raw[pos]
				pos++
			}
		}
	}
	return cur, nil
}
