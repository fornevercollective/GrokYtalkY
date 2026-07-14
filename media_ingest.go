package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Facility / cinema ingest registry for Play Queue.
// Schemes (Blackmagic-first, no per-brand browser plugins):
//
//	ndi:Camera Name          → FFmpeg libndi_newtek (or error + hint)
//	srt://host:port           → raw / HLS restream for browser
//	rtmp(s)://…  rtsp://…     → raw / HLS restream
//	device:0 | device:avfoundation:0
//	decklink:0 | blackmagic:0 → DeckLink (when FFmpeg has decklink)
//	pgm: | pgm:room           → program bus + optional GY_PGM_PLAY_URL
//
// Browser path: facility sources are restreamed to short HLS under
// /api/media/ingest/play/{id}/index.m3u8 (CORS) so queue/TV/Sphere can play them.

const ingestHLSTTL = 45 * time.Minute

// IngestSource is one discoverable input for the queue registry UI.
type IngestSource struct {
	ID     string `json:"id"`               // scheme:ref for resolve
	Scheme string `json:"scheme"`           // ndi|device|decklink|srt|pgm|…
	Label  string `json:"label"`            // human
	Detail string `json:"detail,omitempty"` // driver / hint
	Ready  bool   `json:"ready"`            // ffmpeg can open today
	Brand  string `json:"brand,omitempty"`  // Blackmagic|NDI|Generic|Built-in|External
	Kind   string `json:"kind,omitempty"`   // laptop|front|back|uw|tele|screen|external
	Slot   string `json:"slot,omitempty"`   // preferred scene seat L2|L1|C|R1|R2
	Index  int    `json:"index,omitempty"`  // device index when known
}

type ingestJob struct {
	id      string
	src     string
	dir     string
	cmd     *exec.Cmd
	started time.Time
	playRel string // /api/media/ingest/play/{id}/index.m3u8
}

var (
	ingestMu   sync.Mutex
	ingestJobs = map[string]*ingestJob{}
)

// ParseIngestScheme returns scheme, reference, ok.
// Accepts "ndi:Name", "device:0", "decklink:0", "blackmagic:1", "pgm:", "pgm:dojo",
// and bare srt:// rtmp:// rtsp:// udp:// rtp:// (scheme from URL).
func ParseIngestScheme(src string) (scheme, ref string, ok bool) {
	src = strings.TrimSpace(src)
	if src == "" {
		return "", "", false
	}
	low := strings.ToLower(src)

	// Bare protocol URLs (facility IP)
	for _, p := range []string{"srt://", "rtmp://", "rtmps://", "rtsp://", "rtsps://", "udp://", "tcp://", "rtp://"} {
		if strings.HasPrefix(low, p) {
			sch := strings.TrimSuffix(p, "://")
			if sch == "rtmps" {
				sch = "rtmp"
			}
			if sch == "rtsps" {
				sch = "rtsp"
			}
			return sch, src, true
		}
	}

	// scheme:ref
	i := strings.Index(src, ":")
	if i <= 0 {
		return "", "", false
	}
	scheme = strings.ToLower(src[:i])
	ref = strings.TrimSpace(src[i+1:])
	// strip optional //
	ref = strings.TrimPrefix(ref, "//")

	switch scheme {
	case "ndi", "device", "decklink", "blackmagic", "bmd", "pgm", "program", "cam", "uvc", "three-cam", "threecam":
		if scheme == "bmd" || scheme == "blackmagic" {
			scheme = "decklink"
		}
		if scheme == "program" {
			scheme = "pgm"
		}
		if scheme == "cam" || scheme == "uvc" {
			scheme = "device"
		}
		if scheme == "threecam" {
			scheme = "three-cam"
		}
		return scheme, ref, true
	default:
		return "", "", false
	}
}

// IsIngestSource true when ResolveMedia should use facility / XR path.
func IsIngestSource(src string) bool {
	if _, _, ok := ParseIngestScheme(src); ok {
		return true
	}
	_, _, ok := ParseXRIngest(src)
	return ok
}

// ListIngestSources probes FFmpeg for local devices + static PGM + NDI hint.
func ListIngestSources() []IngestSource {
	var out []IngestSource

	// Program bus tile (always listed)
	pgmURL := strings.TrimSpace(os.Getenv("GY_PGM_PLAY_URL"))
	out = append(out, IngestSource{
		ID:     "pgm:",
		Scheme: "pgm",
		Label:  "PGM · program bus",
		Detail: func() string {
			if pgmURL != "" {
				return pgmURL
			}
			return "set GY_PGM_PLAY_URL or venue publish for live play"
		}(),
		Ready:  pgmURL != "",
		Brand:  "GrokYtalkY",
	})

	// Blackmagic DeckLink (highest leverage pro path)
	if ffmpegHasFormat("decklink") {
		// list formats is sparse; offer common slots
		for i := 0; i < 4; i++ {
			out = append(out, IngestSource{
				ID:     fmt.Sprintf("decklink:%d", i),
				Scheme: "decklink",
				Label:  fmt.Sprintf("DeckLink · %d", i),
				Detail: "Blackmagic DeckLink (FFmpeg decklink)",
				Ready:  true,
				Brand:  "Blackmagic",
			})
		}
	} else {
		out = append(out, IngestSource{
			ID:     "decklink:0",
			Scheme: "decklink",
			Label:  "DeckLink (not in this FFmpeg)",
			Detail: "rebuild FFmpeg with --enable-decklink · Blackmagic Desktop Video",
			Ready:  false,
			Brand:  "Blackmagic",
		})
	}

	// NDI
	if ffmpegHasFormat("libndi_newtek") {
		out = append(out, IngestSource{
			ID:     "ndi:",
			Scheme: "ndi",
			Label:  "NDI · set name",
			Detail: "use ndi:SourceName on LAN (NewTek/Vizrt/BirdDog)",
			Ready:  true,
			Brand:  "NDI",
		})
	} else {
		out = append(out, IngestSource{
			ID:     "ndi:Studio Camera",
			Scheme: "ndi",
			Label:  "NDI (plugin missing)",
			Detail: "FFmpeg --enable-libndi_newtek or NDI Tools → SRT/HLS",
			Ready:  false,
			Brand:  "NDI",
		})
	}

	// Local cameras (UVC / Continuity / FaceTime) — real enumerated names
	for _, d := range listLocalVideoDevices() {
		out = append(out, d)
	}

	// Three-cam preset (built-in C + up to 2 externals)
	if three := ThreeCamSources(); len(three) > 0 {
		ids := make([]string, 0, len(three))
		for _, t := range three {
			ids = append(ids, t.ID)
		}
		out = append([]IngestSource{{
			ID:     "three-cam:" + strings.Join(ids, ","),
			Scheme: "three-cam",
			Label:  fmt.Sprintf("3-cam pack · %d devices", len(three)),
			Detail: "built-in + externals → seats C·L1·R1 · open all in queue",
			Ready:  true,
			Brand:  "GrokYtalkY",
			Kind:   "multi",
			Slot:   "C",
		}}, out...)
	}

	// Example facility URLs
	out = append(out,
		IngestSource{
			ID: "srt://127.0.0.1:9000", Scheme: "srt", Label: "SRT example",
			Detail: "paste real srt://host:port", Ready: true, Brand: "Generic",
		},
	)

	// AR / VR / MR glasses & headsets
	out = append(out, ListXRSources()...)

	return out
}

func listLocalVideoDevices() []IngestSource {
	switch runtime.GOOS {
	case "darwin":
		if list := listAvfoundationVideoDevices(); len(list) > 0 {
			return list
		}
		// fallback indexes if probe fails
		return []IngestSource{{
			ID: "device:avfoundation:0", Scheme: "device", Label: "Camera · avfoundation:0",
			Detail: "macOS (list_devices probe failed)", Ready: ffmpegHasFormat("avfoundation"),
			Brand: "Generic", Kind: "laptop", Slot: "C", Index: 0,
		}}
	case "linux":
		return listV4L2Devices()
	case "windows":
		return []IngestSource{{
			ID: "device:dshow:0", Scheme: "device", Label: "DirectShow camera 0",
			Detail: "device:dshow:Video Name", Ready: ffmpegHasFormat("dshow"),
			Brand: "Generic", Kind: "laptop", Slot: "C",
		}}
	default:
		return nil
	}
}

// listAvfoundationVideoDevices parses `ffmpeg -f avfoundation -list_devices true -i ""`.
// Three-cam pattern: built-in FaceTime → C, then externals → L1/R1, skip pure screen grabs unless only option.
func listAvfoundationVideoDevices() []IngestSource {
	if !ffmpegHasFormat("avfoundation") {
		return nil
	}
	ff, err := lookFFmpeg()
	if err != nil {
		return nil
	}
	// ffmpeg prints device list to stderr and exits non-zero
	cmd := exec.Command(ff, "-hide_banner", "-f", "avfoundation", "-list_devices", "true", "-i", "")
	raw, _ := cmd.CombinedOutput()
	text := string(raw)

	// Only video section lines like: [0] FaceTime HD Camera (Built-in)
	videoSection := false
	type dev struct {
		idx  int
		name string
	}
	var found []dev
	for _, line := range strings.Split(text, "\n") {
		low := strings.ToLower(line)
		if strings.Contains(low, "avfoundation video devices") {
			videoSection = true
			continue
		}
		if strings.Contains(low, "avfoundation audio devices") {
			break
		}
		if !videoSection {
			continue
		}
		// [AVFoundation indev @ …] [0] Name
		if i := strings.LastIndex(line, "]"); i >= 0 && i+1 < len(line) {
			// find [N] before name
			re := regexpDeviceLine.FindStringSubmatch(line)
			if len(re) == 3 {
				idx, _ := strconv.Atoi(re[1])
				name := strings.TrimSpace(re[2])
				if name != "" {
					found = append(found, dev{idx: idx, name: name})
				}
			}
		}
	}
	if len(found) == 0 {
		return nil
	}

	var out []IngestSource
	extN := 0
	for _, d := range found {
		low := strings.ToLower(d.name)
		isScreen := strings.Contains(low, "capture screen") || strings.Contains(low, "screen ")
		isBuiltIn := strings.Contains(low, "facetime") || strings.Contains(low, "built-in") ||
			strings.Contains(low, "built in") || strings.Contains(low, "integrated")
		kind, slot, brand := "external", "R1", "External"
		if isScreen {
			kind, slot, brand = "screen", "R2", "Screen"
		} else if isBuiltIn {
			kind, slot, brand = "laptop", "C", "Built-in"
		} else {
			// external USB / Continuity / phone
			if extN == 0 {
				slot = "L1"
				kind = "front"
			} else if extN == 1 {
				slot = "R1"
				kind = "back"
			} else {
				slot = "L2"
				kind = "tele"
			}
			extN++
			if strings.Contains(low, "continuity") || strings.Contains(low, "iphone") {
				brand = "Continuity"
			} else if strings.Contains(low, "usb") || strings.Contains(low, "cam") {
				brand = "USB"
			}
		}
		out = append(out, IngestSource{
			ID:     fmt.Sprintf("device:avfoundation:%d", d.idx),
			Scheme: "device",
			Label:  d.name,
			Detail: fmt.Sprintf("avfoundation:%d · seat %s · %s", d.idx, slot, kind),
			Ready:  true,
			Brand:  brand,
			Kind:   kind,
			Slot:   slot,
			Index:  d.idx,
		})
	}
	return out
}

// [0] Device Name  (may be prefixed by [AVFoundation indev @ 0x…])
var regexpDeviceLine = regexp.MustCompile(`\[(\d+)\]\s+(.+)$`)

func listV4L2Devices() []IngestSource {
	var out []IngestSource
	for i := 0; i < 8; i++ {
		path := fmt.Sprintf("/dev/video%d", i)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		slot := "C"
		kind := "laptop"
		brand := "V4L2"
		if i == 1 {
			slot, kind = "L1", "front"
		} else if i >= 2 {
			slot, kind = "R1", "external"
		}
		out = append(out, IngestSource{
			ID:     fmt.Sprintf("device:v4l2:%d", i),
			Scheme: "device",
			Label:  fmt.Sprintf("V4L2 · video%d", i),
			Detail: path + " · seat " + slot,
			Ready:  ffmpegHasFormat("v4l2"),
			Brand:  brand,
			Kind:   kind,
			Slot:   slot,
			Index:  i,
		})
	}
	if len(out) == 0 {
		out = append(out, IngestSource{
			ID: "device:v4l2:0", Scheme: "device", Label: "V4L2 · video0",
			Detail: "/dev/video0", Ready: false, Brand: "V4L2", Kind: "laptop", Slot: "C",
		})
	}
	return out
}

// ThreeCamSources returns up to 3 preferred cameras: C built-in + L1 + R1 externals (skip screen).
func ThreeCamSources() []IngestSource {
	all := listLocalVideoDevices()
	var c, sides []IngestSource
	for _, s := range all {
		if s.Kind == "screen" {
			continue
		}
		if s.Slot == "C" || s.Kind == "laptop" {
			if c == nil {
				c = []IngestSource{s}
			}
			continue
		}
		sides = append(sides, s)
	}
	out := []IngestSource{}
	if len(c) > 0 {
		out = append(out, c[0])
	}
	for i := 0; i < len(sides) && len(out) < 3; i++ {
		out = append(out, sides[i])
	}
	// if only one built-in, still return what we have; pad with remaining non-screen
	if len(out) < 3 {
		for _, s := range all {
			if s.Kind == "screen" {
				continue
			}
			dup := false
			for _, o := range out {
				if o.ID == s.ID {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, s)
			}
			if len(out) >= 3 {
				break
			}
		}
	}
	return out
}

// ResolveIngest turns a scheme ref into ResolvedStream.
// Browser consumers should call EnsureIngestBrowserPlay for facility I/O.
func ResolveIngest(src string) (*ResolvedStream, error) {
	// XR / stereo / webxr first
	if _, _, ok := ParseXRIngest(src); ok {
		return ResolveXR(src)
	}
	scheme, ref, ok := ParseIngestScheme(src)
	if !ok {
		return nil, fmt.Errorf("not an ingest scheme")
	}

	switch scheme {
	case "srt", "rtmp", "rtsp", "udp", "tcp", "rtp":
		return &ResolvedStream{
			Input: src, Video: src, Via: "ingest-" + scheme,
			Title: shortURL(src), Format: scheme,
		}, nil

	case "ndi":
		name := ref
		if name == "" {
			return nil, fmt.Errorf("ndi: requires source name (ndi:Camera)")
		}
		if !ffmpegHasFormat("libndi_newtek") {
			return nil, fmt.Errorf("ndi: FFmpeg lacks libndi_newtek — use NDI Tools → SRT, or rebuild FFmpeg with NDI")
		}
		return &ResolvedStream{
			Input: "ndi:" + name, Video: "ndi:" + name, Via: "ingest-ndi",
			Title: "NDI · " + name, Format: "libndi_newtek",
		}, nil

	case "decklink":
		idx := strings.TrimSpace(ref)
		if idx == "" {
			idx = "0"
		}
		if !ffmpegHasFormat("decklink") {
			return nil, fmt.Errorf("decklink: FFmpeg lacks decklink — install Desktop Video + FFmpeg --enable-decklink (Blackmagic-first path)")
		}
		return &ResolvedStream{
			Input: "decklink:" + idx, Video: "decklink:" + idx, Via: "ingest-decklink",
			Title: "DeckLink · " + idx, Format: "decklink",
		}, nil

	case "device":
		dev := normalizeDeviceRef(ref)
		return &ResolvedStream{
			Input: "device:" + dev, Video: "device:" + dev, Via: "ingest-device",
			Title: "Device · " + dev, Format: deviceFormat(dev),
		}, nil

	case "pgm":
		room := ref
		if room == "" {
			room = "global"
		}
		play := strings.TrimSpace(os.Getenv("GY_PGM_PLAY_URL"))
		title := "PGM · " + room
		r := &ResolvedStream{
			Input: "pgm:" + room, Via: "ingest-pgm", Title: title, Format: "program",
		}
		if play != "" {
			r.Video = play
		} else {
			// still "ok" for registry — browser start will explain
			r.Video = ""
		}
		return r, nil

	case "three-cam":
		// Expand happens in StartThreeCam — single resolve returns pack meta
		three := ThreeCamSources()
		if len(three) == 0 {
			return nil, fmt.Errorf("three-cam: no local cameras found")
		}
		ids := make([]string, 0, len(three))
		for _, t := range three {
			ids = append(ids, t.ID)
		}
		return &ResolvedStream{
			Input: "three-cam:" + strings.Join(ids, ","),
			Via:   "ingest-three-cam",
			Title: fmt.Sprintf("3-cam · %d devices", len(three)),
			Format: strings.Join(ids, ","),
		}, nil

	default:
		return nil, fmt.Errorf("unknown ingest scheme %q", scheme)
	}
}

// StartThreeCamHLS starts HLS restreams for up to 3 local cameras (C·L1·R1).
// Returns play URLs for each seat.
func StartThreeCamHLS(publicBase string) ([]map[string]any, error) {
	three := ThreeCamSources()
	if len(three) == 0 {
		return nil, fmt.Errorf("no cameras for three-cam pack")
	}
	var out []map[string]any
	for _, src := range three {
		r, kind, err := EnsureIngestBrowserPlay(src.ID, publicBase)
		item := map[string]any{
			"src": src.ID, "slot": src.Slot, "kind": src.Kind, "label": src.Label,
			"brand": src.Brand, "ready": err == nil,
		}
		if err != nil {
			item["error"] = err.Error()
		} else {
			item["video"] = r.Video
			item["via"] = r.Via
			item["streamKind"] = kind
			item["title"] = r.Title
		}
		out = append(out, item)
	}
	return out, nil
}

func normalizeDeviceRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return defaultDeviceRef()
	}
	// already avfoundation:0 / v4l2:0 / dshow:name
	if strings.Contains(ref, ":") {
		return ref
	}
	// bare index
	if _, err := strconv.Atoi(ref); err == nil {
		switch runtime.GOOS {
		case "darwin":
			return "avfoundation:" + ref
		case "linux":
			return "v4l2:" + ref
		default:
			return "dshow:" + ref
		}
	}
	return ref
}

func defaultDeviceRef() string {
	switch runtime.GOOS {
	case "darwin":
		return "avfoundation:0"
	case "linux":
		return "v4l2:0"
	default:
		return "dshow:0"
	}
}

func deviceFormat(dev string) string {
	if i := strings.Index(dev, ":"); i > 0 {
		return dev[:i]
	}
	return "device"
}

// EnsureIngestBrowserPlay resolves facility sources to a CORS HLS play URL when needed.
// publicBase e.g. http://192.168.0.89:9876
func EnsureIngestBrowserPlay(src, publicBase string) (*ResolvedStream, string, error) {
	r, err := ResolveIngest(src)
	if err != nil {
		return nil, "", err
	}
	// Already HTTP(S) playable (PGM URL, etc.)
	if r.Video != "" && (strings.HasPrefix(r.Video, "http://") || strings.HasPrefix(r.Video, "https://")) {
		video, via, kind := WrapResolvedForBrowser(src, r, publicBase)
		r.Video = video
		if via != "" {
			r.Via = via
		}
		_ = kind
		return r, "http", nil
	}
	// Raw IP protocols / devices → HLS restream
	playRel, err := StartIngestHLS(src)
	if err != nil {
		return r, "", err
	}
	abs := playRel
	if publicBase != "" && strings.HasPrefix(playRel, "/") {
		abs = strings.TrimRight(publicBase, "/") + playRel
	}
	r.Video = abs
	r.Via = r.Via + "+hls"
	return r, "hls", nil
}

// StartIngestHLS runs FFmpeg → fragmented HLS for browser queue/TV.
func StartIngestHLS(src string) (playRel string, err error) {
	r, err := ResolveIngest(src)
	if err != nil {
		return "", err
	}
	// PGM without URL
	if strings.HasPrefix(r.Via, "ingest-pgm") && r.Video == "" {
		return "", fmt.Errorf("pgm: set GY_PGM_PLAY_URL to venue HLS/NDI-proxy URL, or publish via gy venue")
	}
	if r.Video != "" && (strings.HasPrefix(r.Video, "http://") || strings.HasPrefix(r.Video, "https://")) {
		id := MediaPlayRegister(src, r.Video)
		return "/api/media/play/" + id, nil
	}

	ff, err := lookFFmpeg()
	if err != nil {
		return "", err
	}
	inArgs, err := ffmpegIngestInputArgs(src)
	if err != nil {
		return "", err
	}

	ingestMu.Lock()
	// reuse existing job for same source
	for id, job := range ingestJobs {
		if job.src == src && job.cmd != nil && job.cmd.Process != nil {
			// still young enough?
			if time.Since(job.started) < ingestHLSTTL {
				rel := job.playRel
				ingestMu.Unlock()
				return rel, nil
			}
			_ = job.cmd.Process.Kill()
			delete(ingestJobs, id)
		}
	}
	ingestMu.Unlock()

	id := mediaPlayNewID()
	dir := filepath.Join(os.TempDir(), "gy-ingest", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	index := filepath.Join(dir, "index.m3u8")
	seg := filepath.Join(dir, "seg%05d.ts")

	args := append([]string{}, inArgs...)
	args = append(args,
		"-an",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-g", "30",
		"-f", "hls",
		"-hls_time", "1",
		"-hls_list_size", "6",
		"-hls_flags", "delete_segments+append_list",
		"-hls_segment_filename", seg,
		index,
	)

	cmd := exec.Command(ff, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("ffmpeg ingest: %w", err)
	}

	rel := "/api/media/ingest/play/" + id + "/index.m3u8"
	job := &ingestJob{
		id: id, src: src, dir: dir, cmd: cmd, started: time.Now(), playRel: rel,
	}
	ingestMu.Lock()
	ingestJobs[id] = job
	ingestMu.Unlock()

	// wait briefly for playlist
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(index); err == nil && st.Size() > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	go func() {
		_ = cmd.Wait()
		ingestMu.Lock()
		delete(ingestJobs, id)
		ingestMu.Unlock()
	}()

	log.Printf("ingest · hls · %s → %s", src, rel)
	return rel, nil
}

// ffmpegIngestInputArgs builds -f … -i … for a scheme source.
func ffmpegIngestInputArgs(src string) ([]string, error) {
	scheme, ref, ok := ParseIngestScheme(src)
	if !ok {
		return nil, fmt.Errorf("bad ingest src")
	}
	switch scheme {
	case "srt", "rtmp", "rtsp", "udp", "tcp", "rtp":
		return []string{"-i", src}, nil
	case "ndi":
		if ref == "" {
			return nil, fmt.Errorf("ndi name required")
		}
		return []string{"-f", "libndi_newtek", "-i", ref}, nil
	case "decklink":
		// DeckLink device index or name
		name := ref
		if name == "" {
			name = "0"
		}
		// Common pattern: -f decklink -i 'Device @ 0' or numeric — try index form
		return []string{"-f", "decklink", "-i", name}, nil
	case "device":
		dev := normalizeDeviceRef(ref)
		parts := strings.SplitN(dev, ":", 2)
		fmtName := parts[0]
		idx := "0"
		if len(parts) > 1 {
			idx = parts[1]
		}
		switch fmtName {
		case "avfoundation":
			// video:audio — video index only
			return []string{"-f", "avfoundation", "-framerate", "30", "-i", idx + ":none"}, nil
		case "v4l2":
			path := idx
			if !strings.HasPrefix(path, "/") {
				path = "/dev/video" + idx
			}
			return []string{"-f", "v4l2", "-i", path}, nil
		case "dshow":
			return []string{"-f", "dshow", "-i", "video=" + idx}, nil
		default:
			return []string{"-f", fmtName, "-i", idx}, nil
		}
	case "pgm":
		play := strings.TrimSpace(os.Getenv("GY_PGM_PLAY_URL"))
		if play == "" {
			return nil, fmt.Errorf("pgm has no GY_PGM_PLAY_URL")
		}
		return []string{"-i", play}, nil
	default:
		return nil, fmt.Errorf("no ffmpeg input for %s", scheme)
	}
}

func lookFFmpeg() (string, error) {
	for _, name := range []string{"ffmpeg"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("ffmpeg not on PATH")
}

// HandleMediaIngestAPI — list · resolve · start play.
//
//	GET  /api/media/ingest           → {sources:[…], schemes:[…]}
//	GET  /api/media/ingest/resolve?src=ndi:Cam
//	POST /api/media/ingest/start     {"src":"device:0"} → {play, id}
//	GET  /api/media/ingest/play/{id}/…
func HandleMediaIngestAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/media/ingest")
	path = strings.TrimPrefix(path, "/")

	// play/{id}/index.m3u8 or segments
	if strings.HasPrefix(path, "play/") {
		serveIngestPlay(w, r, strings.TrimPrefix(path, "play/"))
		return
	}

	w.Header().Set("Content-Type", "application/json")

	switch {
	case path == "" || path == "list":
		_ = jsonWrite(w, map[string]any{
			"ok":      true,
			"sources": ListIngestSources(),
			"schemes": []string{
				"ndi:", "srt://", "rtmp://", "rtsp://", "device:", "decklink:", "blackmagic:",
				"pgm:", "three-cam:", "xr:", "xr:quest", "xr:vision", "stereo:sbs:", "webxr:",
			},
			"three": ThreeCamSources(),
			"xr":    ListXRSources(),
			"note":  "Blackmagic-first · three-cam · XR/AR/VR/MR glasses via UVC/NDI/SRT/WebXR/cast",
		})
	case path == "resolve":
		src := strings.TrimSpace(r.URL.Query().Get("src"))
		if src == "" {
			src = strings.TrimSpace(r.URL.Query().Get("url"))
		}
		if src == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = jsonWrite(w, map[string]any{"ok": false, "error": "missing src"})
			return
		}
		rsl, err := ResolveIngest(src)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = jsonWrite(w, map[string]any{"ok": false, "error": err.Error(), "src": src})
			return
		}
		_ = jsonWrite(w, map[string]any{
			"ok": true, "src": src, "input": rsl.Input, "video": rsl.Video,
			"title": rsl.Title, "via": rsl.Via, "format": rsl.Format,
			"browser": strings.HasPrefix(rsl.Video, "http"),
		})
	case path == "start":
		src := strings.TrimSpace(r.URL.Query().Get("src"))
		if src == "" && r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if v, ok := body["src"].(string); ok {
				src = strings.TrimSpace(v)
			} else if v, ok := body["url"].(string); ok {
				src = strings.TrimSpace(v)
			}
		}
		if src == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = jsonWrite(w, map[string]any{"ok": false, "error": "missing src"})
			return
		}
		base := mediaPublicBase(r)
		// three-cam pack → multiple HLS plays
		if strings.HasPrefix(strings.ToLower(src), "three-cam") {
			items, err := StartThreeCamHLS(base)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				_ = jsonWrite(w, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			_ = jsonWrite(w, map[string]any{
				"ok": true, "src": src, "multi": true, "items": items,
				"note": "push each item.video into queue · seats C·L1·R1",
			})
			return
		}
		rsl, kind, err := EnsureIngestBrowserPlay(src, base)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = jsonWrite(w, map[string]any{"ok": false, "error": err.Error(), "src": src})
			return
		}
		_ = jsonWrite(w, map[string]any{
			"ok": true, "src": src, "video": rsl.Video, "title": rsl.Title,
			"via": rsl.Via, "streamKind": kind, "play": rsl.Video,
		})
	case path == "three-cam" || path == "threecam":
		base := mediaPublicBase(r)
		items, err := StartThreeCamHLS(base)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = jsonWrite(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = jsonWrite(w, map[string]any{"ok": true, "multi": true, "items": items})
	case path == "pgm":
		room := strings.TrimSpace(r.URL.Query().Get("room"))
		if room == "" {
			room = "global"
		}
		play := strings.TrimSpace(os.Getenv("GY_PGM_PLAY_URL"))
		_ = jsonWrite(w, map[string]any{
			"ok":      true,
			"room":    room,
			"play":    play,
			"src":     "pgm:" + room,
			"ready":   play != "",
			"hint":    "Conductor program bus is mesh type:program; set GY_PGM_PLAY_URL to venue HLS/NDI-proxy for queue PGM tile",
			"schemes": "pgm: · pgm:dojo",
		})
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = jsonWrite(w, map[string]any{"ok": false, "error": "use /api/media/ingest · /resolve · /start · /pgm · /play/{id}/"})
	}
}

func serveIngestPlay(w http.ResponseWriter, r *http.Request, rest string) {
	// rest = {id}/index.m3u8 or {id}/seg00001.ts
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	file := "index.m3u8"
	if len(parts) == 2 && parts[1] != "" {
		file = filepath.Base(parts[1])
	}
	// path safety
	if strings.Contains(file, "..") {
		http.NotFound(w, r)
		return
	}
	ingestMu.Lock()
	job := ingestJobs[id]
	ingestMu.Unlock()
	dir := filepath.Join(os.TempDir(), "gy-ingest", id)
	if job != nil {
		dir = job.dir
	}
	path := filepath.Join(dir, file)
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasSuffix(file, ".m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	} else if strings.HasSuffix(file, ".ts") {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	http.ServeFile(w, r, path)
}

func jsonWrite(w http.ResponseWriter, v any) error {
	enc := json.NewEncoder(w)
	return enc.Encode(v)
}
