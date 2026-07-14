package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ResolvedStream is a playable input for ffmpeg/ffplay after optional yt-dlp resolution.
type ResolvedStream struct {
	// Original user input (URL, path, or share link)
	Input string
	// Title from yt-dlp when available
	Title string
	// Primary media URL/path for video (or combined A/V)
	Video string
	// Optional separate audio URL (yt-dlp often splits YouTube)
	Audio string
	// How it was resolved
	Via string // file | direct | yt-dlp | raw | social
	// yt-dlp format id string used
	Format string
	// Social / live metadata (optional)
	Live     bool            `json:"live,omitempty"`
	Platform string          `json:"platform,omitempty"`
	Handle   string          `json:"handle,omitempty"`
	Mobile   bool            `json:"mobile,omitempty"` // portrait / double-stack glyph
	Lazy     []LazyMediaItem `json:"lazy,omitempty"`   // secondary content for slow lab fill
}

// ResolveMedia turns a path/URL/share-link/handle into ffmpeg-ready stream(s).
// Auto uses yt-dlp for site pages; social handles prefer live/broadcast first.
// Facility schemes: ndi: · srt:// · device: · decklink: · pgm: (see media_ingest.go).
// Passes raw m3u8/mpd/rtsp/etc. straight through.
func ResolveMedia(src string) (*ResolvedStream, error) {
	src = strings.TrimSpace(strings.Trim(src, `"'`))
	src = expandPath(src)
	if src == "" {
		return nil, fmt.Errorf("empty media source")
	}

	// Facility / cinema ingest registry (Blackmagic-first)
	if IsIngestSource(src) {
		return ResolveIngest(src)
	}

	// social handles: @user · twitch:user · yt:@channel · social:@user
	if q := ParseSocialQuery(src); q != nil {
		return ResolveSocial(q)
	}

	// explicit ytdl prefix
	if strings.HasPrefix(src, "ytdl://") || strings.HasPrefix(src, "yt-dlp://") {
		src = strings.TrimPrefix(strings.TrimPrefix(src, "ytdl://"), "yt-dlp://")
		return resolveYtDlp(src)
	}

	// local file
	if !isURL(src) {
		if _, err := os.Stat(src); err != nil {
			return nil, fmt.Errorf("file: %w", err)
		}
		return &ResolvedStream{
			Input: src, Video: src, Via: "file",
			Title: filepath.Base(src),
		}, nil
	}

	// raw streaming protocols — ffmpeg handles natively
	if isRawStreamURL(src) {
		return &ResolvedStream{
			Input: src, Video: src, Via: "raw",
			Title: shortURL(src),
		}, nil
	}

	// direct media URL (has container/ext or m3u8/mpd path)
	if isDirectMediaURL(src) {
		return &ResolvedStream{
			Input: src, Video: src, Via: "direct",
			Title: shortURL(src),
		}, nil
	}

	// site page / share link → blank (when up) then yt-dlp (YouTube, Twitch, X, TikTok, …)
	if needsYtDlp(src) {
		// blank first for TikTok /live and other live pages (cookies + proxy path)
		if blankURLForPage(src) {
			if r, err := ResolveViaBlank(src); err == nil && r != nil && r.Video != "" {
				return r, nil
			}
		}
		if isLivePageURL(src) {
			if r, err := resolveYtDlpLiveFirst(src); err == nil {
				return r, nil
			}
			// last chance blank if live-first yt-dlp failed
			if r, err := ResolveViaBlank(src); err == nil && r != nil && r.Video != "" {
				return r, nil
			}
		}
		return resolveYtDlp(src)
	}

	// unknown https — try yt-dlp first, then direct
	if r, err := resolveYtDlp(src); err == nil {
		return r, nil
	}
	return &ResolvedStream{
		Input: src, Video: src, Via: "direct",
		Title: shortURL(src),
	}, nil
}

// blankURLForPage true when blank is the preferred resolve path (TikTok, live pages).
func blankURLForPage(src string) bool {
	if BlankBaseURL() == "" || !BlankReachable(BlankBaseURL()) {
		return false
	}
	low := strings.ToLower(src)
	if strings.Contains(low, "tiktok.com") {
		return true
	}
	return isLivePageURL(src)
}

func isRawStreamURL(s string) bool {
	low := strings.ToLower(s)
	for _, p := range []string{
		"rtsp://", "rtsps://", "rtmp://", "rtmps://",
		"srt://", "udp://", "tcp://", "rtp://",
		"whip://", "whep://",
	} {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

func isDirectMediaURL(s string) bool {
	low := strings.ToLower(s)
	// strip query for ext check
	path := low
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	for _, ext := range []string{
		".mp4", ".mkv", ".mov", ".webm", ".avi", ".m4v", ".ts",
		".m3u8", ".mpd", ".flv", ".wmv", ".gif", ".ogv", ".mpeg", ".mpg",
		".aac", ".mp3", ".opus", ".wav", ".flac",
	} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	// common CDN progressive patterns
	if strings.Contains(low, "/playlist.m3u8") || strings.Contains(low, ".m3u8?") {
		return true
	}
	if strings.Contains(low, ".mpd?") || strings.HasSuffix(path, ".mpd") {
		return true
	}
	return false
}

func needsYtDlp(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")
	// known extractors (subset — yt-dlp has 1800+; these force resolve)
	sites := []string{
		"youtube.com", "youtu.be", "youtube-nocookie.com",
		"twitch.tv", "clips.twitch.tv",
		"vimeo.com", "dailymotion.com",
		"twitter.com", "x.com", "t.co",
		"tiktok.com", "vm.tiktok.com",
		"instagram.com", "facebook.com", "fb.watch",
		"soundcloud.com", "bandcamp.com",
		"bilibili.com", "nicovideo.jp",
		"reddit.com", "v.redd.it",
		"streamable.com", "rumble.com",
		"kick.com", "dlive.tv",
		"pornhub.com", // extractor exists; keep generic
	}
	for _, s := range sites {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	// youtu.be short
	if host == "youtu.be" {
		return true
	}
	return false
}

func isLivePageURL(s string) bool {
	low := strings.ToLower(s)
	for _, k := range []string{
		"/live", "twitch.tv/", "kick.com/", "/streams", "is_live",
		"broadcast", "livestream",
	} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

func resolveYtDlp(pageURL string) (*ResolvedStream, error) {
	return resolveYtDlpQuality(pageURL, "")
}

// resolveYtDlpQuality picks format ladder.
// quality "best" | "1080" | "max" → high-res TV path; default stays ≤720 terminal-friendly.
func resolveYtDlpQuality(pageURL, quality string) (*ResolvedStream, error) {
	bin, err := lookYtDlp()
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(strings.TrimSpace(quality))
	var formats []string
	switch q {
	case "best", "max", "1080", "tv", "hi", "high":
		// TV / queue path: prefer 1080p then best merged, then plain best
		formats = []string{
			"bv*[height<=1080]+ba/b[height<=1080]",
			"bv*+ba/b",
			"bestvideo*+bestaudio/best",
			"best",
		}
	default:
		// Prefer combined progressive ≤720p for light terminal pipe;
		// fall back to best video+audio merge URLs, then plain best.
		formats = []string{
			"bv*[height<=720][ext=mp4]+ba[ext=m4a]/b[height<=720]/bv*+ba/b",
			"best[height<=720]/best",
			"bv*+ba/b",
			"best",
		}
	}

	var lastErr error
	for _, f := range formats {
		r, err := ytDlpGetURLs(bin, pageURL, f)
		if err != nil {
			lastErr = err
			continue
		}
		title := ytDlpTitle(bin, pageURL)
		if title == "" {
			title = shortURL(pageURL)
		}
		r.Input = pageURL
		r.Title = title
		r.Via = "yt-dlp"
		r.Format = f
		r.Live = ytDlpIsLive(bin, pageURL)
		r.Mobile = looksPortraitTitle(title) || isMobileHostURL(pageURL)
		return r, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("yt-dlp: %w", lastErr)
	}
	return nil, fmt.Errorf("yt-dlp: no playable formats")
}

// resolveYtDlpLiveFirst prefers HLS / live-friendly formats for broadcast pages.
func resolveYtDlpLiveFirst(pageURL string) (*ResolvedStream, error) {
	bin, err := lookYtDlp()
	if err != nil {
		return nil, err
	}
	r, err := resolveYtDlpSocial(bin, pageURL, true)
	if err != nil {
		return nil, err
	}
	r.Via = "yt-dlp"
	if plat := platformFromURL(pageURL); plat != "" {
		r.Platform = plat
		r.Mobile = IsMobileSocialPlatform(plat)
	}
	return r, nil
}

func isMobileHostURL(s string) bool {
	low := strings.ToLower(s)
	for _, h := range []string{"tiktok.com", "instagram.com", "vm.tiktok.com", "youtube.com/shorts"} {
		if strings.Contains(low, h) {
			return true
		}
	}
	return false
}

func platformFromURL(s string) string {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "twitch.tv"):
		return SocialTwitch
	case strings.Contains(low, "youtube.com"), strings.Contains(low, "youtu.be"):
		return SocialYouTube
	case strings.Contains(low, "kick.com"):
		return SocialKick
	case strings.Contains(low, "tiktok.com"):
		return SocialTikTok
	case strings.Contains(low, "instagram.com"):
		return SocialInstagram
	case strings.Contains(low, "x.com"), strings.Contains(low, "twitter.com"):
		return SocialX
	case strings.Contains(low, "rumble.com"):
		return SocialRumble
	case strings.Contains(low, "facebook.com"):
		return SocialFacebook
	default:
		return ""
	}
}

func lookYtDlp() (string, error) {
	for _, name := range []string{"yt-dlp", "yt_dlp", "youtube-dl"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	// common brew/local paths
	for _, p := range []string{
		"/usr/local/bin/yt-dlp",
		"/opt/homebrew/bin/yt-dlp",
		filepath.Join(os.Getenv("HOME"), ".local/bin/yt-dlp"),
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("yt-dlp not found — brew install yt-dlp (or pipx install yt-dlp)")
}

func ytDlpGetURLs(bin, pageURL, format string) (*ResolvedStream, error) {
	// -g: print direct URL(s). May be 1 line (muxed) or 2 (video then audio).
	args := []string{
		"--no-playlist",
		"--no-warnings",
		"-f", format,
		"-g",
		"--", pageURL,
	}
	cmd := exec.Command(bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", truncate(msg, 200))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var urls []string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln != "" && !strings.HasPrefix(ln, "WARNING") {
			urls = append(urls, ln)
		}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("empty -g output")
	}
	r := &ResolvedStream{Video: urls[0]}
	if len(urls) > 1 {
		r.Audio = urls[1]
	}
	return r, nil
}

func ytDlpTitle(bin, pageURL string) string {
	cmd := exec.Command(bin,
		"--no-playlist", "--no-warnings",
		"--print", "%(title).80s",
		"--", pageURL,
	)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func shortURL(s string) string {
	if len(s) <= 48 {
		return s
	}
	return s[:45] + "…"
}

// StreamDoctor checks ffmpeg / ffprobe / ffplay / yt-dlp on PATH.
func StreamDoctor() string {
	var b strings.Builder
	check := func(name string) {
		if p, err := exec.LookPath(name); err == nil {
			ver := toolVersion(p)
			fmt.Fprintf(&b, "  ✓ %s  %s\n", name, ver)
		} else {
			fmt.Fprintf(&b, "  ✗ %s  missing\n", name)
		}
	}
	b.WriteString("stream tools:\n")
	check("ffmpeg")
	check("ffprobe")
	check("ffplay")
	if p, err := lookYtDlp(); err == nil {
		fmt.Fprintf(&b, "  ✓ yt-dlp  %s (%s)\n", toolVersion(p), p)
	} else {
		b.WriteString("  ✗ yt-dlp  missing — gy install deps -y  (brew|uv|pipx)\n")
	}
	// package managers summary (short)
	ms := DetectPackageManagers()
	if len(ms) > 0 {
		b.WriteString("pkg managers: ")
		var names []string
		for _, m := range ms {
			names = append(names, string(m.ID))
		}
		b.WriteString(strings.Join(names, " · "))
		b.WriteString("\n")
	}
	// media supervisor
	b.WriteString(FormatMediaHealthDetail(Media().Health()))
	return b.String()
}

func toolVersion(bin string) string {
	cmd := exec.Command(bin, "--version")
	out, err := cmd.Output()
	if err != nil {
		// ffmpeg prints to stderr
		cmd = exec.Command(bin, "-version")
		out, err = cmd.CombinedOutput()
		if err != nil {
			return ""
		}
	}
	line := strings.SplitN(string(out), "\n", 2)[0]
	line = strings.TrimSpace(line)
	if len(line) > 60 {
		line = line[:60]
	}
	return line
}

// EnsureYtDlp tries to install yt-dlp via brew if missing (best-effort, no sudo).
func EnsureYtDlp() error {
	if _, err := lookYtDlp(); err == nil {
		return nil
	}
	if brew, err := exec.LookPath("brew"); err == nil {
		cmd := exec.Command(brew, "install", "yt-dlp")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return fmt.Errorf("yt-dlp missing and brew not found")
}

// resolveTimeout wrapper for UI so resolve doesn't hang forever.
func ResolveMediaTimeout(src string, d time.Duration) (*ResolvedStream, error) {
	type result struct {
		r   *ResolvedStream
		err error
	}
	ch := make(chan result, 1)
	go func() {
		r, err := ResolveMedia(src)
		ch <- result{r, err}
	}()
	select {
	case res := <-ch:
		return res.r, res.err
	case <-time.After(d):
		return nil, fmt.Errorf("resolve timeout after %s (yt-dlp slow?)", d)
	}
}
