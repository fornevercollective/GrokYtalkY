package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// News wall — GrokGlyph-style multi-tile live broadcaster mosaic in the terminal lab.
// Each tile is a low-rate RGB glyph stream (matrix/hex/braille/ascii/blocks) from a
// major agency live page (yt-dlp → ffmpeg). Loads staggered so the mesh stays responsive.

const (
	MaxNewsWallFeeds = 12
	// per-tile capture budget (light enough for a wall of 8–12)
	newsTileW   = 64
	newsTileH   = 36
	newsTileFPS = 3
)

// NewsSource is one broadcaster live page (prefer /live URLs for yt-dlp).
type NewsSource struct {
	ID     string // short slug for labels
	Label  string // display name
	URL    string // yt-dlp / page URL
	Region string // world | us | eu | me | asia
}

// MajorNewsSources — default terminal wall fill (first N of extended catalog).
// Full world catalog (news + weather + public access) lives in site/news-sources.js
// for livenews.html; terminal keeps a focused default for ffmpeg budget.
func MajorNewsSources() []NewsSource {
	all := ExtendedNewsSources()
	if len(all) > MaxNewsWallFeeds {
		return all[:MaxNewsWallFeeds]
	}
	return all
}

// ExtendedNewsSources — broader world list for /news all and gy doctor / docs.
// Prefer /live URLs for yt-dlp. Streams may geo-restrict or go offline.
func ExtendedNewsSources() []NewsSource {
	return []NewsSource{
		// US news
		{ID: "abc", Label: "ABC News", URL: "https://www.youtube.com/@ABCNews/live", Region: "us"},
		{ID: "nbc", Label: "NBC News", URL: "https://www.youtube.com/@NBCNews/live", Region: "us"},
		{ID: "cbs", Label: "CBS News", URL: "https://www.youtube.com/@CBSNews/live", Region: "us"},
		{ID: "cnn", Label: "CNN", URL: "https://www.youtube.com/@CNN/live", Region: "us"},
		{ID: "fox", Label: "Fox News", URL: "https://www.youtube.com/@FoxNews/live", Region: "us"},
		{ID: "msnbc", Label: "MSNBC", URL: "https://www.youtube.com/@MSNBC/live", Region: "us"},
		{ID: "bbg", Label: "Bloomberg", URL: "https://www.youtube.com/@BloombergTelevision/live", Region: "us"},
		{ID: "cnbc", Label: "CNBC", URL: "https://www.youtube.com/@CNBC/live", Region: "us"},
		{ID: "cspan", Label: "C-SPAN", URL: "https://www.youtube.com/@cspan/live", Region: "us"},
		{ID: "pbs", Label: "PBS NewsHour", URL: "https://www.youtube.com/@PBSNewsHour/live", Region: "us"},
		// Europe / UK / ME / Asia / world
		{ID: "bbc", Label: "BBC News", URL: "https://www.youtube.com/@BBCNews/live", Region: "uk"},
		{ID: "sky", Label: "Sky News", URL: "https://www.youtube.com/@SkyNews/live", Region: "uk"},
		{ID: "eur", Label: "Euronews", URL: "https://www.youtube.com/@euronews/live", Region: "eu"},
		{ID: "f24", Label: "France 24", URL: "https://www.youtube.com/@France24_en/live", Region: "eu"},
		{ID: "dw", Label: "DW News", URL: "https://www.youtube.com/@dwnews/live", Region: "eu"},
		{ID: "aje", Label: "Al Jazeera", URL: "https://www.youtube.com/@AlJazeeraEnglish/live", Region: "me"},
		{ID: "nhk", Label: "NHK World", URL: "https://www.youtube.com/@NHKWORLDJAPAN/live", Region: "asia"},
		{ID: "cna", Label: "CNA", URL: "https://www.youtube.com/@channelnewsasia/live", Region: "asia"},
		{ID: "reu", Label: "Reuters", URL: "https://www.youtube.com/@Reuters/live", Region: "world"},
		// weather
		{ID: "weatherch", Label: "Weather Channel", URL: "https://www.youtube.com/@TheWeatherChannel/live", Region: "weather"},
		{ID: "accu", Label: "AccuWeather", URL: "https://www.youtube.com/@AccuWeather/live", Region: "weather"},
		{ID: "foxweather", Label: "FOX Weather", URL: "https://www.youtube.com/@FOXWeather/live", Region: "weather"},
		{ID: "nhc", Label: "NHC", URL: "https://www.youtube.com/@NWSNHC/live", Region: "weather"},
		// public access / gov
		{ID: "nasa", Label: "NASA Live", URL: "https://www.youtube.com/@NASA/live", Region: "public"},
		{ID: "un", Label: "United Nations", URL: "https://www.youtube.com/@UN/live", Region: "world"},
		{ID: "house", Label: "US House", URL: "https://www.youtube.com/@HouseofRepresentatives/live", Region: "public"},
		{ID: "nyctv", Label: "NYC Media", URL: "https://www.youtube.com/@NYCMedia/live", Region: "public"},
		{ID: "cbc", Label: "CBC News", URL: "https://www.youtube.com/@CBCNews/live", Region: "ca"},
		{ID: "abcau", Label: "ABC News AU", URL: "https://www.youtube.com/@abcnewsaustralia/live", Region: "oceania"},
		// EarthCam-style landmarks (full catalog: site/news-sources.js)
		{ID: "earthcam", Label: "EarthCam Live", URL: "https://www.youtube.com/@EarthCam/live", Region: "earthcam"},
		{ID: "skylinewebcams", Label: "SkylineWebcams", URL: "https://www.youtube.com/@SkylineWebcams/live", Region: "earthcam"},
		{ID: "ec-timessq", Label: "Times Square", URL: "https://www.youtube.com/@EarthCam/live", Region: "earthcam-us"},
		{ID: "ec-ggbridge", Label: "Golden Gate", URL: "https://www.youtube.com/results?search_query=golden+gate+bridge+live+cam", Region: "earthcam-us"},
		{ID: "ec-eiffel", Label: "Eiffel Tower", URL: "https://www.youtube.com/results?search_query=eiffel+tower+live+cam", Region: "earthcam-eu"},
		{ID: "ec-shibuya", Label: "Shibuya", URL: "https://www.youtube.com/results?search_query=shibuya+crossing+live+cam", Region: "earthcam-asia"},
		{ID: "ec-traffic-la", Label: "LA Freeways", URL: "https://www.youtube.com/results?search_query=los+angeles+freeway+live+cam", Region: "earthcam-highway"},
	}
}

// NewsWallStyleLadder — GrokGlyph site styles mapped onto terminal PixelMode.
// Cycles per-tile so the wall reads like grokglyph matrix|blocks|braille|ascii|hex.
var NewsWallStyleLadder = []PixelMode{
	PixelHalf,    // matrix / truecolor face
	PixelHex,     // hex mosaic
	PixelBraille, // density
	PixelASCII,   // shade ramp
	PixelBlocks,  // chunky blocks
	PixelScan,    // CRT scan (broadcast vibe)
	PixelNeon,    // edge bloom
	PixelDither,  // ordered green terminal
}

// NewsWallStyleName matches GrokGlyph vocabulary for status/help.
func NewsWallStyleName(m PixelMode) string {
	switch m {
	case PixelHalf:
		return "matrix"
	case PixelHex:
		return "hex"
	case PixelBraille:
		return "braille"
	case PixelASCII:
		return "ascii"
	case PixelBlocks:
		return "blocks"
	case PixelScan:
		return "scan"
	case PixelNeon:
		return "neon"
	case PixelDither:
		return "dither"
	default:
		return m.String()
	}
}

// NewsTileOpts is optional geometry/rate for StartNewsTile (vision retune path).
type NewsTileOpts struct {
	W   int // pixels; 0 = default newsTileW
	H   int
	FPS int // 0 = default newsTileFPS
}

// NewsTilePipe is a lightweight per-broadcaster RGB capture (low FPS, small frame).
type NewsTilePipe struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	cancel  chan struct{}
	running bool
	Label   string
	Src     string
	Frame   *FramePixels
	Err     string
	Style   PixelMode
	mediaID string
	// encode geometry (vision retune)
	W   int
	H   int
	FPS int
	// recovery
	Restarts int
	lastDie  time.Time
	Poster   *FramePixels // last good or branded poster for soft recovery
}

// StartNewsTile opens a throttled ffmpeg rawvideo pipe from a resolved media URL.
// Registers with Media() supervisor (backpressure + kill-on-exit).
func StartNewsTile(label, videoURL string, style PixelMode) (*NewsTilePipe, error) {
	return StartNewsTileOpts(label, videoURL, style, NewsTileOpts{})
}

// StartNewsTileOpts same as StartNewsTile with explicit scale/fps (vision control plane).
func StartNewsTileOpts(label, videoURL string, style PixelMode, opts NewsTileOpts) (*NewsTilePipe, error) {
	if videoURL == "" {
		return nil, fmt.Errorf("empty video url")
	}
	if !Media().CanSpawn(MediaKindNews) {
		return nil, fmt.Errorf("news wall at capacity (max %d tiles)", Media().NewsMax())
	}
	w, h, fps := opts.W, opts.H, opts.FPS
	if w <= 0 {
		w = newsTileW
	}
	if h <= 0 {
		h = newsTileH
	}
	if fps <= 0 {
		fps = newsTileFPS
	}
	if w < 32 {
		w = 32
	}
	if w > 320 {
		w = 320
	}
	if h < 18 {
		h = 18
	}
	if h > 180 {
		h = 180
	}
	if h%2 != 0 {
		h++
	}
	if fps < 1 {
		fps = 1
	}
	if fps > 15 {
		fps = 15
	}
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "3",
		"-rw_timeout", "15000000", // 15s I/O timeout (µs)
		"-i", videoURL,
		"-an",
		"-vf", fmt.Sprintf("scale=%d:%d:flags=fast_bilinear,fps=%d,format=rgb24", w, h, fps),
		"-f", "rawvideo", "-pix_fmt", "rgb24",
		"pipe:1",
	}
	cmd := exec.Command("ffmpeg", args...)
	PrepMediaCmd(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	mid, err := Media().Register(MediaKindNews, label, cmd)
	if err != nil {
		return nil, err
	}
	poster := newsPoster(label, "", 0)
	tp := &NewsTilePipe{
		cmd:     cmd,
		cancel:  make(chan struct{}),
		running: true,
		Label:   label,
		Src:     videoURL,
		Style:   style,
		mediaID: mid,
		W:       w,
		H:       h,
		FPS:     fps,
		Poster:  poster,
		Frame:   poster.Clone(),
	}
	go tp.readLoop(stdout, w, h)
	return tp, nil
}

func (tp *NewsTilePipe) readLoop(r io.ReadCloser, w, h int) {
	defer r.Close()
	frameSize := w * h * 3
	buf := make([]byte, frameSize)
	for {
		select {
		case <-tp.cancel:
			return
		default:
		}
		if _, err := io.ReadFull(r, buf); err != nil {
			tp.mu.Lock()
			tp.Err = err.Error()
			tp.running = false
			tp.lastDie = time.Now()
			// soft recovery: keep poster / last frame visible
			if tp.Frame == nil && tp.Poster != nil {
				tp.Frame = tp.Poster.Clone()
			}
			tp.mu.Unlock()
			return
		}
		cp := make([]byte, frameSize)
		copy(cp, buf)
		tp.mu.Lock()
		tp.Frame = &FramePixels{
			W: w, H: h, RGB: cp,
			Source: "news:" + tp.Label,
			Stamp:  time.Now().UnixMilli(),
		}
		tp.running = true
		tp.Err = ""
		mid := tp.mediaID
		tp.mu.Unlock()
		if mid != "" {
			Media().Heartbeat(mid)
		}
	}
}

// Snapshot returns a clone of the latest frame (or poster).
func (tp *NewsTilePipe) Snapshot() *FramePixels {
	if tp == nil {
		return nil
	}
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if tp.Frame != nil {
		return tp.Frame.Clone()
	}
	if tp.Poster != nil {
		return tp.Poster.Clone()
	}
	return nil
}

// Healthy is true when pipe is running and recently produced frames.
func (tp *NewsTilePipe) Healthy() bool {
	if tp == nil {
		return false
	}
	tp.mu.Lock()
	defer tp.mu.Unlock()
	return tp.running && tp.Err == ""
}

// NeedsRestart is true after unexpected death (soft recovery candidate).
func (tp *NewsTilePipe) NeedsRestart() bool {
	if tp == nil {
		return false
	}
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if tp.running || tp.Src == "" {
		return false
	}
	// backoff: at least 4s since death, max 8 restarts (vision control plane may add)
	if tp.Restarts >= 8 {
		return false
	}
	if tp.lastDie.IsZero() {
		return true
	}
	return time.Since(tp.lastDie) > 4*time.Second
}

// Stop kills the capture process via media supervisor.
func (tp *NewsTilePipe) Stop() {
	if tp == nil {
		return
	}
	tp.mu.Lock()
	if tp.cancel != nil {
		select {
		case <-tp.cancel:
		default:
			close(tp.cancel)
		}
	}
	mid := tp.mediaID
	cmd := tp.cmd
	tp.cmd = nil
	tp.mediaID = ""
	tp.running = false
	tp.mu.Unlock()
	if mid != "" {
		Media().Kill(mid)
	} else if cmd != nil {
		_ = killCmd(cmd)
	}
}

// RestartNewsTile stops old and returns a new supervised pipe (soft recovery).
func RestartNewsTile(old *NewsTilePipe) (*NewsTilePipe, error) {
	if old == nil {
		return nil, fmt.Errorf("nil tile")
	}
	old.mu.Lock()
	label, src, style := old.Label, old.Src, old.Style
	opts := NewsTileOpts{W: old.W, H: old.H, FPS: old.FPS}
	restarts := old.Restarts + 1
	poster := old.Poster
	old.mu.Unlock()
	if src == "" {
		return nil, fmt.Errorf("nothing to restart")
	}
	if restarts > 8 {
		return nil, fmt.Errorf("%s: max restarts", label)
	}
	old.Stop()
	nt, err := StartNewsTileOpts(label, src, style, opts)
	if err != nil {
		return nil, err
	}
	nt.mu.Lock()
	nt.Restarts = restarts
	if poster != nil {
		nt.Poster = poster
	}
	nt.mu.Unlock()
	return nt, nil
}

// RetuneNewsTile restarts the tile with new ffmpeg scale/fps (vision control plane).
func RetuneNewsTile(old *NewsTilePipe, opts NewsTileOpts) (*NewsTilePipe, error) {
	if old == nil {
		return nil, fmt.Errorf("nil tile")
	}
	old.mu.Lock()
	label, src, style := old.Label, old.Src, old.Style
	if opts.W <= 0 {
		opts.W = old.W
	}
	if opts.H <= 0 {
		opts.H = old.H
	}
	if opts.FPS <= 0 {
		opts.FPS = old.FPS
	}
	restarts := old.Restarts + 1
	poster := old.Poster
	old.mu.Unlock()
	if src == "" {
		return nil, fmt.Errorf("nothing to retune")
	}
	old.Stop()
	nt, err := StartNewsTileOpts(label, src, style, opts)
	if err != nil {
		return nil, err
	}
	nt.mu.Lock()
	nt.Restarts = restarts
	if poster != nil {
		nt.Poster = poster
	}
	nt.mu.Unlock()
	return nt, nil
}

// NewsWallState orchestrates multi-agency glyph tiles inside LabState.
type NewsWallState struct {
	On      bool
	Pipes   []*NewsTilePipe
	Sources []NewsSource
	// StyleBase first style; tiles get StyleBase+i for GrokGlyph variety
	StyleBase PixelMode
	loading   bool
	// auto soft-restart dead tiles
	AutoRecover bool
}

// newsPoster builds a branded placeholder until the live pipe delivers frames.
func newsPoster(label, region string, seed int) *FramePixels {
	w, h := newsTileW, newsTileH
	if h%2 != 0 {
		h++
	}
	rgb := make([]byte, w*h*3)
	// region tint
	pr, pg, pb := byte(20), byte(28), byte(48)
	switch strings.ToLower(region) {
	case "us":
		pr, pg, pb = 30, 40, 90
	case "eu":
		pr, pg, pb = 20, 50, 90
	case "me":
		pr, pg, pb = 70, 40, 20
	case "asia":
		pr, pg, pb = 40, 20, 50
	case "world":
		pr, pg, pb = 25, 55, 45
	}
	// phase by seed
	pr = byte(min(255, int(pr)+seed*7%40))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			fade := float64(y) / float64(h)
			// subtle scan
			scan := 1.0
			if y%3 == 0 {
				scan = 0.55
			}
			rgb[i] = byte(float64(pr) * (0.4 + 0.6*fade) * scan)
			rgb[i+1] = byte(float64(pg) * (0.4 + 0.6*fade) * scan)
			rgb[i+2] = byte(float64(pb) * (0.4 + 0.6*fade) * scan)
		}
	}
	// live badge bar
	for y := 0; y < 3; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			if x < 10 {
				rgb[i], rgb[i+1], rgb[i+2] = 200, 30, 30
			} else {
				rgb[i] = byte(min(255, int(pr)+30))
				rgb[i+1] = byte(min(255, int(pg)+20))
				rgb[i+2] = byte(min(255, int(pb)+20))
			}
		}
	}
	_ = label
	return &FramePixels{W: w, H: h, RGB: rgb, Source: "news-poster:" + label, Stamp: time.Now().UnixMilli()}
}

// FilterNewsSources returns sources matching region (empty/"all" = all).
func FilterNewsSources(region string, maxN int) []NewsSource {
	// use extended catalog so weather/public/regional filters work
	all := ExtendedNewsSources()
	region = strings.ToLower(strings.TrimSpace(region))
	var out []NewsSource
	for _, s := range all {
		if region != "" && region != "all" && region != "world" {
			if s.Region != region && !(region == "intl" && (s.Region == "eu" || s.Region == "me" || s.Region == "asia" || s.Region == "uk")) {
				continue
			}
		}
		out = append(out, s)
		if maxN > 0 && len(out) >= maxN {
			break
		}
	}
	if len(out) == 0 {
		// fallback full set
		out = all
		if maxN > 0 && len(out) > maxN {
			out = out[:maxN]
		}
	}
	return out
}

// NewsWallStagger is delay between starting each broadcaster pipe.
func NewsWallStagger() time.Duration {
	return 2200 * time.Millisecond
}
