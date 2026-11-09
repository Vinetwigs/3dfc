// Copyright (C) 2026 Vinetwigs
// SPDX-License-Identifier: GPL-3.0-only

package ff

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	_ "image/png"
	"os"
	"path/filepath"
)

func SplitAlpha(srcDir string, nframes int, dstDir string) error {
	for i := 0; i < nframes; i++ {
		f, err := os.Open(filepath.Join(srcDir, fmt.Sprintf("gop_%06d.png", i)))
		if err != nil {
			return err
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return err
		}
		b := img.Bounds()
		gray := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				gray.SetGray(x, y, color.Gray{Y: uint8(a >> 8)})
			}
		}
		out, err := os.Create(filepath.Join(dstDir, fmt.Sprintf("a_%06d.png", i)))
		if err != nil {
			return err
		}
		err = png.Encode(out, gray)
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
