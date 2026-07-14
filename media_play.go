package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Built-in blank-lite tools: yt-dlp resolve is already in stream.go;
// this file adds the CORS HLS play proxy browsers need for canvas sampling
// (same role as blank /api/ingest/play + /api/ingest/proxy).
//
// Live News works with only `gy serve` + yt-dlp on PATH — no node blank server.

const mediaPlayTTL = 45 * time.Minute

type mediaPlayRow struct {
	streamURL string
	pageURL   string
	allowed   map[string]struct{}
	created   time.Time
}

var (
	mediaPlayMu    sync.Mutex
	mediaPlayCache = map[string]*mediaPlayRow{}
)

func mediaPlayNewID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func mediaPlayPrune() {
	now := time.Now()
	for id, row := range mediaPlayCache {
		if now.Sub(row.created) > mediaPlayTTL {
			delete(mediaPlayCache, id)
		}
	}
}

// MediaPlayRegister stores a raw stream URL and returns a play id.
func MediaPlayRegister(pageURL, streamURL string) string {
	mediaPlayMu.Lock()
	defer mediaPlayMu.Unlock()
	mediaPlayPrune()
	id := mediaPlayNewID()
	mediaPlayCache[id] = &mediaPlayRow{
		streamURL: streamURL,
		pageURL:   pageURL,
		allowed:   map[string]struct{}{streamURL: {}},
		created:   time.Now(),
	}
	return id
}

func mediaPlayGet(id string) *mediaPlayRow {
	mediaPlayMu.Lock()
	defer mediaPlayMu.Unlock()
	mediaPlayPrune()
	return mediaPlayCache[id]
}

func mediaPlayAllow(id, abs string) {
	mediaPlayMu.Lock()
	defer mediaPlayMu.Unlock()
	if row := mediaPlayCache[id]; row != nil {
		row.allowed[abs] = struct{}{}
	}
}

func mediaPlayAllowed(target string) (*mediaPlayRow, bool) {
	mediaPlayMu.Lock()
	defer mediaPlayMu.Unlock()
	mediaPlayPrune()
	for _, row := range mediaPlayCache {
		if _, ok := row.allowed[target]; ok {
			return row, true
		}
	}
	return nil, false
}

func isHLSLikeURL(u string) bool {
	low := strings.ToLower(u)
	if strings.Contains(low, ".m3u8") {
		return true
	}
	if strings.Contains(low, "manifest.googlevideo") || strings.Contains(low, "playlist_type/dvr") {
		return true
	}
	if strings.Contains(low, "/api/ingest/play/") || strings.Contains(low, "/api/media/play/") {
		return true
	}
	return false
}

// WrapResolvedForBrowser rewrites HLS (and blank play URLs that we re-resolve)
// to hub /api/media/play/{id} so the browser can CORS-sample frames.
// Returns videoURL, via, streamKind.
func WrapResolvedForBrowser(pageURL string, src *ResolvedStream, publicBase string) (video, via, kind string) {
	if src == nil || src.Video == "" {
		return "", "", ""
	}
	video = src.Video
	via = src.Via
	kind = ""

	// Already hub-proxied absolute or relative
	if strings.Contains(video, "/api/media/play/") {
		if strings.HasPrefix(video, "/") && publicBase != "" {
			video = strings.TrimRight(publicBase, "/") + video
		}
		return video, via, "hls"
	}

	// Blank proxy URL — only useful while blank is up; prefer raw if we can keep it
	// by re-resolving is heavier. If it's blank-proxy, pass through when blank reachable,
	// else fall through (caller should have raw from yt-dlp).
	if strings.Contains(video, "/api/ingest/play/") {
		if BlankReachable(BlankBaseURL()) {
			return video, via, "hls"
		}
		// blank down and we only have proxy URL — cannot unwrap
		return video, via, "hls"
	}

	if isHLSLikeURL(video) || looksM3U8Path(video) {
		id := MediaPlayRegister(pageURL, video)
		path := "/api/media/play/" + id
		if publicBase != "" {
			video = strings.TrimRight(publicBase, "/") + path
		} else {
			video = path
		}
		via = "hub-proxy"
		if src.Via != "" && src.Via != "hub-proxy" {
			via = src.Via + "+hub-proxy"
		}
		return video, via, "hls"
	}

	// Progressive / direct MP4 — still proxy for canvas CORS on some CDNs
	if isDirectMediaURL(video) && (strings.HasPrefix(video, "http://") || strings.HasPrefix(video, "https://")) {
		id := MediaPlayRegister(pageURL, video)
		path := "/api/media/play/" + id
		if publicBase != "" {
			video = strings.TrimRight(publicBase, "/") + path
		} else {
			video = path
		}
		via = "hub-proxy"
		if src.Via != "" {
			via = src.Via + "+hub-proxy"
		}
		return video, via, "direct"
	}

	return video, via, kind
}

func looksM3U8Path(u string) bool {
	path := u
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	return strings.HasSuffix(strings.ToLower(path), ".m3u8")
}

func mediaPublicBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// reverse proxies
	if xf := r.Header.Get("X-Forwarded-Proto"); xf == "https" || xf == "http" {
		scheme = xf
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1:9876"
	}
	return scheme + "://" + host
}

func mediaCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Range")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, Content-Range, Accept-Ranges")
}

func mediaUpstreamHeaders(pageURL string) http.Header {
	h := make(http.Header)
	h.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	h.Set("Accept", "*/*")
	if pageURL != "" {
		if u, err := url.Parse(pageURL); err == nil {
			h.Set("Referer", pageURL)
			h.Set("Origin", u.Scheme+"://"+u.Host)
		}
	}
	return h
}

func mediaFetchUpstream(target string, pageURL string) (*http.Response, error) {
	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return fmt.Errorf("too many redirects")
			}
			// keep UA/referer on redirects
			for k, vv := range mediaUpstreamHeaders(pageURL) {
				if req.Header.Get(k) == "" {
					for _, v := range vv {
						req.Header.Add(k, v)
					}
				}
			}
			return nil
		},
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header = mediaUpstreamHeaders(pageURL)
	return client.Do(req)
}

// rewriteM3u8 rewrites playlist lines to hub proxy URLs and records allowed URLs.
func rewriteM3u8(body, baseURL, playID, publicBase string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			// URI="..." inside tags (EXT-X-KEY, EXT-X-MAP, …)
			if strings.Contains(t, "URI=\"") {
				out = append(out, rewriteM3u8URIAttr(line, baseURL, playID, publicBase))
			} else {
				out = append(out, line)
			}
			continue
		}
		abs, err := url.Parse(t)
		if err != nil {
			out = append(out, line)
			continue
		}
		if !abs.IsAbs() {
			base, err2 := url.Parse(baseURL)
			if err2 != nil {
				out = append(out, line)
				continue
			}
			abs = base.ResolveReference(abs)
		}
		absS := abs.String()
		mediaPlayAllow(playID, absS)
		proxy := strings.TrimRight(publicBase, "/") + "/api/media/proxy?u=" + url.QueryEscape(absS)
		out = append(out, proxy)
	}
	return strings.Join(out, "\n")
}

func rewriteM3u8URIAttr(line, baseURL, playID, publicBase string) string {
	// minimal: replace URI="..."
	const key = `URI="`
	i := strings.Index(line, key)
	if i < 0 {
		return line
	}
	start := i + len(key)
	end := strings.Index(line[start:], `"`)
	if end < 0 {
		return line
	}
	raw := line[start : start+end]
	abs, err := url.Parse(raw)
	if err != nil {
		return line
	}
	if !abs.IsAbs() {
		base, err2 := url.Parse(baseURL)
		if err2 != nil {
			return line
		}
		abs = base.ResolveReference(abs)
	}
	absS := abs.String()
	mediaPlayAllow(playID, absS)
	proxy := strings.TrimRight(publicBase, "/") + "/api/media/proxy?u=" + url.QueryEscape(absS)
	return line[:start] + proxy + line[start+end:]
}

// HandleMediaPlay serves GET /api/media/play/{id}
func HandleMediaPlay(w http.ResponseWriter, r *http.Request) {
	mediaCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/media/play/")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "bad play id", http.StatusBadRequest)
		return
	}
	row := mediaPlayGet(id)
	if row == nil {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "play session expired — resolve again\n", http.StatusGone)
		return
	}
	up, err := mediaFetchUpstream(row.streamURL, row.pageURL)
	if err != nil {
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer up.Body.Close()

	// Read a peek for m3u8 detection (small playlists); stream rest if binary.
	const peekN = 64 << 10
	peek := make([]byte, peekN)
	n, _ := io.ReadFull(up.Body, peek)
	if n < 0 {
		n = 0
	}
	peek = peek[:n]
	ct := up.Header.Get("Content-Type")
	head := string(peek)
	if len(head) > 32 {
		head = head[:32]
	}
	isPlaylist := isHLSLikeURL(row.streamURL) ||
		strings.Contains(strings.ToLower(ct), "mpegurl") ||
		strings.Contains(strings.ToLower(ct), "m3u8") ||
		strings.HasPrefix(strings.TrimSpace(string(peek)), "#EXTM3U")

	pub := mediaPublicBase(r)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if up.StatusCode >= 400 {
		w.WriteHeader(up.StatusCode)
		if r.Method != http.MethodHead {
			_, _ = w.Write(peek)
			_, _ = io.Copy(w, up.Body)
		}
		return
	}

	if isPlaylist {
		rest, _ := io.ReadAll(io.LimitReader(up.Body, 2<<20))
		body := append(peek, rest...)
		text := rewriteM3u8(string(body), row.streamURL, id, pub)
		out := []byte(text)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(out)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(out)
		}
		return
	}

	// binary / progressive: stream peek + rest
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if cl := up.Header.Get("Content-Length"); cl != "" && n == len(peek) {
		// only if we didn't truncate peek oddly — skip exact CL when partial
	}
	w.WriteHeader(up.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	if n > 0 {
		_, _ = w.Write(peek)
	}
	_, _ = io.Copy(w, up.Body)
}

// HandleMediaProxy serves GET /api/media/proxy?u=https://…
func HandleMediaProxy(w http.ResponseWriter, r *http.Request) {
	mediaCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := strings.TrimSpace(r.URL.Query().Get("u"))
	if u == "" || (!strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://")) {
		http.Error(w, "bad proxy url", http.StatusBadRequest)
		return
	}
	row, ok := mediaPlayAllowed(u)
	if !ok {
		http.Error(w, "url not in active play session", http.StatusForbidden)
		return
	}
	page := ""
	if row != nil {
		page = row.pageURL
	}
	up, err := mediaFetchUpstream(u, page)
	if err != nil {
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer up.Body.Close()

	// Nested playlists need rewrite too
	ct := up.Header.Get("Content-Type")
	peek := make([]byte, 64<<10)
	n, _ := io.ReadFull(up.Body, peek)
	if n < 0 {
		n = 0
	}
	peek = peek[:n]
	isPlaylist := strings.Contains(strings.ToLower(ct), "mpegurl") ||
		strings.Contains(strings.ToLower(u), ".m3u8") ||
		strings.HasPrefix(strings.TrimSpace(string(peek)), "#EXTM3U")

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if isPlaylist && row != nil {
		// need play id for allow-list — find id by matching stream
		playID := mediaPlayIDForRow(row)
		rest, _ := io.ReadAll(io.LimitReader(up.Body, 2<<20))
		body := append(peek, rest...)
		text := rewriteM3u8(string(body), u, playID, mediaPublicBase(r))
		out := []byte(text)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(out)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(out)
		}
		return
	}

	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if ar := up.Header.Get("Accept-Ranges"); ar != "" {
		w.Header().Set("Accept-Ranges", ar)
	}
	w.WriteHeader(up.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	if n > 0 {
		_, _ = w.Write(peek)
	}
	_, _ = io.Copy(w, up.Body)
}

func mediaPlayIDForRow(want *mediaPlayRow) string {
	mediaPlayMu.Lock()
	defer mediaPlayMu.Unlock()
	for id, row := range mediaPlayCache {
		if row == want {
			return id
		}
	}
	// fallback: register under new id if lost (shouldn't happen)
	id := mediaPlayNewID()
	mediaPlayCache[id] = want
	return id
}

func resolveMediaForBrowserTimeout(src string, d time.Duration) (*ResolvedStream, error) {
	return resolveMediaForBrowserTimeoutQuality(src, d, "")
}

func resolveMediaForBrowserTimeoutQuality(src string, d time.Duration, quality string) (*ResolvedStream, error) {
	type result struct {
		r   *ResolvedStream
		err error
	}
	ch := make(chan result, 1)
	go func() {
		r, err := resolveMediaForBrowserQuality(src, quality)
		ch <- result{r, err}
	}()
	select {
	case res := <-ch:
		return res.r, res.err
	case <-time.After(d):
		return nil, fmt.Errorf("resolve timeout after %s (yt-dlp slow?)", d)
	}
}

// resolveMediaForBrowser prefers local yt-dlp (then hub CORS proxy) over the
// optional node blank server. blank remains a fallback for cookie-gated TikTok etc.
func resolveMediaForBrowser(src string) (*ResolvedStream, error) {
	return resolveMediaForBrowserQuality(src, "")
}

func resolveMediaForBrowserQuality(src, quality string) (*ResolvedStream, error) {
	src = strings.TrimSpace(strings.Trim(src, `"'`))
	if src == "" {
		return nil, fmt.Errorf("empty media source")
	}
	// Expand t.co short links are handled by yt-dlp when needsYtDlp
	if q := ParseSocialQuery(src); q != nil {
		return ResolveSocial(q)
	}
	if needsYtDlp(src) {
		wantBest := quality == "best" || quality == "max" || quality == "1080" || quality == "tv" || quality == "hi" || quality == "high"
		if isLivePageURL(src) || strings.Contains(strings.ToLower(src), "x.com/i/spaces") {
			if r, err := resolveYtDlpLiveFirst(src); err == nil && r != nil && r.Video != "" {
				return r, nil
			}
		}
		if wantBest {
			if r, err := resolveYtDlpQuality(src, "best"); err == nil && r != nil && r.Video != "" {
				return r, nil
			}
		}
		if r, err := resolveYtDlp(src); err == nil && r != nil && r.Video != "" {
			return r, nil
		}
		// optional blank fallback (TikTok cookies / when yt-dlp fails)
		if r, err := ResolveViaBlank(src); err == nil && r != nil && r.Video != "" {
			return r, nil
		}
		// last error from yt-dlp path
		if wantBest {
			return resolveYtDlpQuality(src, "best")
		}
		if isLivePageURL(src) {
			return resolveYtDlpLiveFirst(src)
		}
		return resolveYtDlp(src)
	}
	return ResolveMedia(src)
}

// FormatMediaToolsDoctor reports built-in blank-lite status for gy doctor.
func FormatMediaToolsDoctor() string {
	var b strings.Builder
	b.WriteString("media tools (built-in · blank-lite)\n")
	b.WriteString("  resolve   gy yt-dlp · /api/media/resolve\n")
	b.WriteString("  play      /api/media/play/{id}  CORS HLS proxy\n")
	b.WriteString("  proxy     /api/media/proxy?u=… segment rewrite\n")
	if p, err := lookYtDlp(); err == nil {
		fmt.Fprintf(&b, "  yt-dlp    %s (%s)\n", toolVersion(p), p)
	} else {
		b.WriteString("  yt-dlp    missing — gy install deps -y\n")
	}
	mediaPlayMu.Lock()
	n := len(mediaPlayCache)
	mediaPlayMu.Unlock()
	fmt.Fprintf(&b, "  sessions  %d active play\n", n)
	b.WriteString("  note      Live News works without node blank when yt-dlp is installed\n")
	return b.String()
}
