package ff

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func Available() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func x264Preset(preset string) string {
	if preset == "fast" {
		return "veryfast"
	}
	return "veryslow"
}

func EncodeGOP(dir string, nframes, fps int, preset string, withAlpha bool) (rgb, alpha []byte, err error) {
	p := x264Preset(preset)
	rgbOut := filepath.Join(dir, "seg_rgb.mkv")
	args := []string{"-y", "-v", "error",
		"-framerate", fmt.Sprint(fps),
		"-i", filepath.Join(dir, "gop_%06d.png"),
		"-frames:v", fmt.Sprint(nframes),
		"-c:v", "libx264rgb", "-qp", "0", "-preset", p, "-pix_fmt", "rgb24",
		rgbOut}
	if out, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
		return nil, nil, fmt.Errorf("ffmpeg rgb: %v (%s)", e, out)
	}
	rgb, err = os.ReadFile(rgbOut)
	if err != nil {
		return nil, nil, err
	}
	if !withAlpha {
		return rgb, nil, nil
	}
	grayDir, err := os.MkdirTemp(dir, "alpha-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(grayDir)
	if err := SplitAlpha(dir, nframes, grayDir); err != nil {
		return nil, nil, err
	}
	alphaOut := filepath.Join(dir, "seg_a.mkv")
	args = []string{"-y", "-v", "error",
		"-framerate", fmt.Sprint(fps),
		"-i", filepath.Join(grayDir, "a_%06d.png"),
		"-frames:v", fmt.Sprint(nframes),
		"-c:v", "libx264", "-qp", "0", "-preset", p, "-pix_fmt", "gray",
		alphaOut}
	if out, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
		return nil, nil, fmt.Errorf("ffmpeg alpha: %v (%s)", e, out)
	}
	alpha, err = os.ReadFile(alphaOut)
	if err != nil {
		return nil, nil, err
	}
	return rgb, alpha, nil
}

func DecodeGOP(workdir string, rgb, alpha []byte, nframes int) error {
	if err := os.WriteFile(filepath.Join(workdir, "seg_rgb.mkv"), rgb, 0644); err != nil {
		return err
	}
	args := []string{"-y", "-v", "error", "-i", filepath.Join(workdir, "seg_rgb.mkv"),
		filepath.Join(workdir, "out_%06d.png")}
	if out, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
		return fmt.Errorf("ffmpeg decode rgb: %v (%s)", e, out)
	}
	if len(alpha) == 0 {
		return nil
	}
	if err := os.WriteFile(filepath.Join(workdir, "seg_a.mkv"), alpha, 0644); err != nil {
		return err
	}
	args = []string{"-y", "-v", "error", "-i", filepath.Join(workdir, "seg_a.mkv"),
		filepath.Join(workdir, "aout_%06d.png")}
	if out, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
		return fmt.Errorf("ffmpeg decode alpha: %v (%s)", e, out)
	}
	return nil
}
