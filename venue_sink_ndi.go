package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// NDIVenueSink — real NDI egress via FFmpeg libndi_newtek when available.
// Lattice glyphs nearest-neighbor scaled to HD; program meta written beside stream.
//
//	gy venue --sink ndi --ndi-name "GrokYtalkY-PGM"
//
// Requires FFmpeg built with --enable-libndi_newtek (or NDI SDK plugin).
// Without it, falls back to MPEG-TS UDP + program JSON sidecar (still a live sink).

// NDIOpts configures NDI (or NDI-fallback) egress.
type NDIOpts struct {
	Name    string // NDI source name on the LAN
	Width   int
	Height  int
	FPS     int
	Quiet   bool
	MetaDir string // directory for program.json sidecar (default: os.TempDir/gy-venue)
	// FallbackUDP when libndi_newtek missing, e.g. udp://127.0.0.1:1234
	FallbackUDP string
}

// NewNDIVenueSink builds an NDI (or fallback) VenueSink.
func NewNDIVenueSink(opts NDIOpts) (VenueSink, error) {
	if opts.Name == "" {
		opts.Name = "GrokYtalkY-PGM"
	}
	if opts.Width < 16 {
		opts.Width = VenueDefaultW
	}
	if opts.Height < 16 {
		opts.Height = VenueDefaultH
	}
	if opts.FPS < 1 {
		opts.FPS = VenueDefaultFPS
	}
	if opts.MetaDir == "" {
		opts.MetaDir = filepath.Join(os.TempDir(), "gy-venue")
	}
	_ = os.MkdirAll(opts.MetaDir, 0o755)
	metaPath := filepath.Join(opts.MetaDir, "ndi-program.json")

	args := ffmpegRawInputArgs(opts.Width, opts.Height, opts.FPS)
	label := "ndi"
	if ffmpegHasFormat("libndi_newtek") {
		// true NDI
		args = append(args,
			"-c:v", "rawvideo",
			"-pix_fmt", "uyvy422",
			"-f", "libndi_newtek",
			"-ndi_name", opts.Name,
			"-",
		)
		if !opts.Quiet {
			log.Printf("venue · ndi · libndi_newtek name=%q", opts.Name)
		}
	} else {
		// graceful live fallback — still a real network sink for lab/venue pipelines
		udp := opts.FallbackUDP
		if udp == "" {
			udp = "udp://127.0.0.1:13000?pkt_size=1316"
		}
		args = append(args,
			"-c:v", "libx264",
			"-preset", "ultrafast",
			"-tune", "zerolatency",
			"-pix_fmt", "yuv420p",
			"-g", fmt.Sprintf("%d", opts.FPS),
			"-f", "mpegts",
			udp,
		)
		label = "ndi-fallback-mpegts"
		if !opts.Quiet {
			log.Printf("venue · ndi · libndi_newtek not in ffmpeg — fallback MPEG-TS %s", udp)
			log.Printf("venue · ndi · install FFmpeg+NDI SDK for true NDI; program meta → %s", metaPath)
		}
	}

	s := newFFmpegPipeSink(label, "ndi", opts.Width, opts.Height, opts.FPS, args, opts.Quiet)
	s.metaPath = metaPath
	return s, nil
}
