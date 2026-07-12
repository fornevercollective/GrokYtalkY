package main

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CLI: gy encode <src> [out]   — file/URL/cam-frame → gyst|gyhex|pcap
//      gy decode <stream>      — inspect binary/hex/pcap stream

func runEncodeCLI(args []string) error {
	if len(args) < 1 {
		fmt.Println(`usage:
  gy encode video.mp4 [out.gyst|out.gyhex|out.pcap]
  gy encode frame.jpg out.gyhex
  gy encode in.gyst out.pcap     # re-wrap`)
		return nil
	}
	src := expandPath(args[0])
	out := ""
	if len(args) >= 2 {
		out = expandPath(args[1])
	}
	format := "gyst"
	if out != "" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(out)), ".")
		if format == "hex" {
			format = "gyhex"
		}
		if format == "bin" || format == "gybin" {
			format = "gyst"
		}
	} else {
		base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		out = base + ".gyst"
	}

	// already a stream file — re-wrap
	if IsStreamCodecPath(src) || DetectStreamFile(src) != "unknown" {
		pkts, err := LoadStreamFile(src)
		if err != nil {
			return err
		}
		return exportPackets(out, format, pkts)
	}

	// still image
	if IsImagePath(src) {
		fp, err := LoadImageFile(src, 160, 96)
		if err != nil {
			return err
		}
		p := PacketFromFramePixels(fp, 1)
		return exportPackets(out, format, []StreamPacket{p})
	}

	// sample video via ffmpeg → few RGB frames
	pkts, err := sampleVideoToPackets(src, 24, 80, 48)
	if err != nil {
		return err
	}
	if err := exportPackets(out, format, pkts); err != nil {
		return err
	}
	fmt.Printf("encoded %d packets → %s (%s)\n", len(pkts), out, format)
	return nil
}

func exportPackets(path, format string, pkts []StreamPacket) error {
	switch format {
	case "gyhex", "hex":
		return WriteGyHexFile(path, pkts, map[string]string{
			"packets": fmt.Sprintf("%d", len(pkts)),
			"app":     "grokytalky",
		})
	case "pcap":
		return WritePCAP(path, pkts)
	default:
		return WriteGystFile(path, pkts)
	}
}

// sampleVideoToPackets extracts up to maxFrames RGB24 frames via ffmpeg.
func sampleVideoToPackets(src string, maxFrames, w, h int) ([]StreamPacket, error) {
	if w < 8 {
		w = 80
	}
	if h < 4 {
		h = 48
	}
	if h%2 != 0 {
		h++
	}
	// resolve yt if needed
	r, err := ResolveMediaTimeout(src, 90*time.Second)
	if err != nil {
		return nil, err
	}
	in := r.Video
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-i", in,
		"-an",
		"-vf", fmt.Sprintf("scale=%d:%d:flags=bicubic,fps=12,format=rgb24", w, h),
		"-vframes", fmt.Sprintf("%d", maxFrames),
		"-f", "rawvideo",
		"-pix_fmt", "rgb24",
		"pipe:1",
	}
	cmd := exec.Command("ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	frameSize := w * h * 3
	buf := make([]byte, frameSize)
	var pkts []StreamPacket
	t0 := uint64(time.Now().UnixMilli())
	for i := 0; i < maxFrames; i++ {
		if _, err := io.ReadFull(stdout, buf); err != nil {
			break
		}
		cp := make([]byte, frameSize)
		copy(cp, buf)
		pkts = append(pkts, PacketFromRGB(cp, w, h, uint32(i+1), t0+uint64(i)*83))
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	if len(pkts) == 0 {
		return nil, fmt.Errorf("no frames decoded from %s", src)
	}
	return pkts, nil
}

func runDecodeCLI(args []string) error {
	if len(args) < 1 {
		fmt.Println(`usage: gy decode stream.gyst|gyhex|pcap
  prints packet table; play with: gy watch stream.gyst`)
		return nil
	}
	path := expandPath(args[0])
	fmt := DetectStreamFile(path)
	pkts, err := LoadStreamFile(path)
	if err != nil {
		return err
	}
	printStreamInfo(path, fmt, pkts)
	return nil
}

func printStreamInfo(path, format string, pkts []StreamPacket) {
	fmt.Printf("file:    %s\n", path)
	fmt.Printf("format:  %s\n", format)
	fmt.Printf("packets: %d\n", len(pkts))
	var rgb, pcm, jpeg, hexn, meta int
	for _, p := range pkts {
		switch p.Kind {
		case KindRGB24:
			rgb++
		case KindPCM16:
			pcm++
		case KindJPEG:
			jpeg++
		case KindHexLum:
			hexn++
		default:
			meta++
		}
	}
	fmt.Printf("  rgb24=%d  jpeg=%d  hexlum=%d  pcm16=%d  meta=%d\n", rgb, jpeg, hexn, pcm, meta)
	n := len(pkts)
	if n > 8 {
		n = 8
	}
	fmt.Println("head:")
	for i := 0; i < n; i++ {
		p := pkts[i]
		fmt.Printf("  #%d %s %dx%d seq=%d ts=%d payload=%d\n",
			i, p.KindName(), p.Width, p.Height, p.Seq, p.TimeMS, len(p.Payload))
	}
	if len(pkts) > 8 {
		fmt.Printf("  … %d more\n", len(pkts)-8)
	}
	fmt.Printf("play:  gy watch %s\n", path)
}
