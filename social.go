package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Social platforms for handle → stream resolution (live/broadcast first).
const (
	SocialTwitch    = "twitch"
	SocialYouTube   = "youtube"
	SocialKick      = "kick"
	SocialTikTok    = "tiktok"
	SocialX         = "x"
	SocialInstagram = "instagram"
	SocialRumble    = "rumble"
	SocialFacebook  = "facebook"
)

// LazyMediaItem is secondary content resolved after the primary live/broadcast.
// Loaded into lab slots slowly so the live path stays responsive.
type LazyMediaItem struct {
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Kind     string `json:"kind,omitempty"` // live | vod | clip | post | short
	Platform string `json:"platform,omitempty"`
	Mobile   bool   `json:"mobile,omitempty"`
}

// SocialQuery is a parsed social handle request.
type SocialQuery struct {
	Raw      string
	Platform string // empty = probe live-first across platforms
	Handle   string // bare username without @
}

// socialHandleRe matches @user, platform:user, platform/@user, platform/user
var (
	reSocialPrefixed = regexp.MustCompile(`(?i)^(twitch|tw|ttv|youtube|yt|youtu|kick|tiktok|tt|twitter|x|instagram|ig|rumble|facebook|fb)[:/]+@?([A-Za-z0-9._-]{2,64})$`)
	reSocialAt       = regexp.MustCompile(`^@([A-Za-z0-9._-]{2,64})$`)
	reSocialBare     = regexp.MustCompile(`(?i)^(twitch|tw|ttv|youtube|yt|kick|tiktok|tt|x|twitter|instagram|ig|rumble)/@?([A-Za-z0-9._-]{2,64})$`)
)

// ParseSocialQuery detects handle-style sources. Returns nil if not social syntax.
func ParseSocialQuery(src string) *SocialQuery {
	src = strings.TrimSpace(strings.Trim(src, `"'`))
	if src == "" {
		return nil
	}
	// already a full URL — not handle syntax (ResolveMedia handles pages)
	if isURL(src) {
		return nil
	}
	// strip leading "social:" / "handle:"
	low := strings.ToLower(src)
	for _, p := range []string{"social:", "handle:", "user:"} {
		if strings.HasPrefix(low, p) {
			src = strings.TrimSpace(src[len(p):])
			low = strings.ToLower(src)
			break
		}
	}
	if m := reSocialPrefixed.FindStringSubmatch(src); len(m) == 3 {
		return &SocialQuery{Raw: src, Platform: normalizeSocialPlatform(m[1]), Handle: strings.TrimPrefix(m[2], "@")}
	}
	if m := reSocialBare.FindStringSubmatch(src); len(m) == 3 {
		return &SocialQuery{Raw: src, Platform: normalizeSocialPlatform(m[1]), Handle: strings.TrimPrefix(m[2], "@")}
	}
	if m := reSocialAt.FindStringSubmatch(src); len(m) == 2 {
		return &SocialQuery{Raw: src, Platform: "", Handle: m[1]}
	}
	return nil
}

func normalizeSocialPlatform(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "twitch", "tw", "ttv":
		return SocialTwitch
	case "youtube", "yt", "youtu":
		return SocialYouTube
	case "kick":
		return SocialKick
	case "tiktok", "tt":
		return SocialTikTok
	case "twitter", "x":
		return SocialX
	case "instagram", "ig":
		return SocialInstagram
	case "rumble":
		return SocialRumble
	case "facebook", "fb":
		return SocialFacebook
	default:
		return strings.ToLower(p)
	}
}

// socialLiveCandidates returns page URLs ordered live/broadcast first, then profile/VOD hubs.
func socialLiveCandidates(platform, handle string) []string {
	h := strings.TrimPrefix(handle, "@")
	if h == "" {
		return nil
	}
	switch platform {
	case SocialTwitch:
		return []string{
			"https://www.twitch.tv/" + h,
			"https://www.twitch.tv/" + h + "/videos",
		}
	case SocialYouTube:
		return []string{
			"https://www.youtube.com/@" + h + "/live",
			"https://www.youtube.com/@" + h + "/streams",
			"https://www.youtube.com/@" + h + "/videos",
			"https://www.youtube.com/@" + h,
			"https://www.youtube.com/c/" + h,
			"https://www.youtube.com/" + h,
		}
	case SocialKick:
		return []string{
			"https://kick.com/" + h,
			"https://kick.com/" + h + "/videos",
		}
	case SocialTikTok:
		return []string{
			"https://www.tiktok.com/@" + h + "/live",
			"https://www.tiktok.com/@" + h,
		}
	case SocialX:
		return []string{
			"https://x.com/" + h,
			"https://twitter.com/" + h,
		}
	case SocialInstagram:
		return []string{
			"https://www.instagram.com/" + h + "/",
			"https://www.instagram.com/" + h + "/reels/",
		}
	case SocialRumble:
		return []string{
			"https://rumble.com/c/" + h,
			"https://rumble.com/user/" + h,
		}
	case SocialFacebook:
		return []string{
			"https://www.facebook.com/" + h + "/live",
			"https://www.facebook.com/" + h,
		}
	default:
		return nil
	}
}

// platforms for bare @handle probe — live-first order.
func socialProbePlatforms() []string {
	return []string{
		SocialTwitch,
		SocialYouTube,
		SocialKick,
		SocialTikTok,
		SocialRumble,
		SocialX,
		SocialInstagram,
	}
}

// IsMobileSocialPlatform prefers portrait / double-stack GrokGlyph scale.
func IsMobileSocialPlatform(platform string) bool {
	switch normalizeSocialPlatform(platform) {
	case SocialTikTok, SocialInstagram, SocialX:
		return true
	default:
		return false
	}
}

// MobileGlyphStackSize returns sample W×H for mobile content at double-stack GrokGlyph scale.
// One glyph face is N×N; double stack is N × 2N (portrait 1:2), scaled for terminal paint.
func MobileGlyphStackSize(glyphN int) (w, h int) {
	if glyphN < 13 {
		glyphN = 25
	}
	if glyphN > 49 {
		glyphN = 49
	}
	// 4 px per glyph cell → readable half-blocks; height = 2 stacks
	w = glyphN * 4
	h = glyphN * 8 // double stack
	if h%2 != 0 {
		h++
	}
	return w, h
}

// MobilePortraitHalfRows maps glyph double-stack to terminal half-block rows for a given width.
func MobilePortraitHalfRows(cols, glyphN int) int {
	if cols < 8 {
		cols = 8
	}
	if glyphN < 13 {
		glyphN = 25
	}
	// double-stack aspect ~ 1:2 → half-rows ≈ cols (pixel H = 2*cols for 1:2 width:height)
	// clamp to glyph double-stack proportion
	half := cols // ~1:2 in half-block space (each row = 2px)
	maxHalf := glyphN * 2 // two glyph faces tall in “logical” cells
	if half > maxHalf*2 {
		half = maxHalf * 2
	}
	if half < 6 {
		half = 6
	}
	return half
}

// ResolveSocial expands a handle to a playable stream: live/broadcast first, then other content.
// Secondary items are attached as Lazy for staggered lab fill.
// Prefers blank POST /api/ingest/resolve when blank is up (TikTok live parity).
func ResolveSocial(q *SocialQuery) (*ResolvedStream, error) {
	if q == nil || q.Handle == "" {
		return nil, fmt.Errorf("empty social handle")
	}
	bin, err := lookYtDlp()
	// blank can resolve even if yt-dlp missing on gy PATH (blank has its own)
	blankUp := BlankBaseURL() != "" && BlankReachable(BlankBaseURL())
	if err != nil && !blankUp {
		return nil, err
	}

	platforms := socialProbePlatforms()
	if q.Platform != "" {
		platforms = []string{q.Platform}
	}

	var lastErr error
	var bestFallback *ResolvedStream

	for _, plat := range platforms {
		cands := socialLiveCandidates(plat, q.Handle)
		for i, page := range cands {
			liveBias := i == 0 // first candidate is live/broadcast page

			// Prefer blank for TikTok live / social pages when server is up
			if blankUp && (plat == SocialTikTok || liveBias || plat == SocialInstagram) {
				if r, berr := ResolveViaBlank(page); berr == nil && r != nil && r.Video != "" {
					r.Input = q.Raw
					if r.Input == "" {
						r.Input = page
					}
					r.Platform = plat
					r.Handle = q.Handle
					r.Mobile = IsMobileSocialPlatform(plat) || looksPortraitTitle(r.Title)
					if r.Via == "" {
						r.Via = "blank-social"
					}
					if r.Live || liveBias {
						if bin != "" {
							r.Lazy = socialLazyList(bin, plat, q.Handle, page, 6)
						}
						if r.Title == "" {
							r.Title = plat + "/@" + q.Handle
						}
						return r, nil
					}
					if bestFallback == nil {
						bestFallback = r
					}
					continue
				} else if berr != nil {
					lastErr = berr
				}
			}

			if bin == "" {
				continue
			}
			r, err := resolveYtDlpSocial(bin, page, liveBias)
			if err != nil {
				lastErr = err
				continue
			}
			r.Input = q.Raw
			if r.Input == "" {
				r.Input = page
			}
			r.Platform = plat
			r.Handle = q.Handle
			r.Mobile = IsMobileSocialPlatform(plat) || looksPortraitTitle(r.Title)
			r.Via = "social"
			if r.Live || liveBias {
				// attach lazy secondary from same creator (slow path)
				r.Lazy = socialLazyList(bin, plat, q.Handle, page, 6)
				if r.Title == "" {
					r.Title = plat + "/@" + q.Handle
				}
				return r, nil
			}
			// keep first non-live as fallback while probing for live
			if bestFallback == nil {
				bestFallback = r
				bestFallback.Lazy = socialLazyList(bin, plat, q.Handle, page, 6)
			}
		}
		// platform-pinned: accept first successful even if not live
		if q.Platform != "" && bestFallback != nil {
			return bestFallback, nil
		}
	}
	if bestFallback != nil {
		return bestFallback, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("social @%s: %w", q.Handle, lastErr)
	}
	return nil, fmt.Errorf("social @%s: no live or playable content (try platform:handle)", q.Handle)
}

func looksPortraitTitle(title string) bool {
	low := strings.ToLower(title)
	for _, k := range []string{"#shorts", "reel", "tiktok", "vertical", "mobile", "9:16"} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

// socialFormatPrefs: live/broadcast friendly formats first, then mobile progressive, then best.
func socialFormatPrefs(liveBias bool) []string {
	if liveBias {
		return []string{
			// live HLS / best available
			"best[protocol^=m3u8]/best[protocol*=http]/best",
			"bv*+ba/b",
			"best",
		}
	}
	// mobile-friendly progressive first for VOD/reels
	return []string{
		"bv*[height<=720][ext=mp4]+ba[ext=m4a]/b[height<=720][ext=mp4]/b[height<=720]",
		"best[height<=720]/best",
		"bv*+ba/b",
		"best",
	}
}

func resolveYtDlpSocial(bin, pageURL string, liveBias bool) (*ResolvedStream, error) {
	var lastErr error
	for _, f := range socialFormatPrefs(liveBias) {
		r, err := ytDlpGetURLs(bin, pageURL, f)
		if err != nil {
			lastErr = err
			continue
		}
		title := ytDlpTitle(bin, pageURL)
		live := ytDlpIsLive(bin, pageURL)
		r.Input = pageURL
		r.Title = title
		if r.Title == "" {
			r.Title = shortURL(pageURL)
		}
		r.Via = "yt-dlp"
		r.Format = f
		r.Live = live
		if liveBias && !live {
			// still return playable; caller may prefer another candidate
			return r, nil
		}
		return r, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no formats for %s", shortURL(pageURL))
}

func ytDlpIsLive(bin, pageURL string) bool {
	cmd := exec.Command(bin,
		"--no-playlist", "--no-warnings",
		"--print", "%(is_live)s",
		"--", pageURL,
	)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	v := strings.ToLower(strings.TrimSpace(string(out)))
	return v == "1" || v == "true" || v == "yes"
}

// socialLazyList fetches flat playlist / related entries for slow lab fill (excludes primary page).
func socialLazyList(bin, platform, handle, primaryPage string, maxN int) []LazyMediaItem {
	if maxN < 1 {
		maxN = 4
	}
	if maxN > 8 {
		maxN = 8
	}
	// pick a “more content” hub after live
	hubs := socialLiveCandidates(platform, handle)
	var hub string
	for _, u := range hubs {
		if u != primaryPage {
			hub = u
			break
		}
	}
	if hub == "" {
		hub = primaryPage
	}
	args := []string{
		"--flat-playlist",
		"--no-warnings",
		"--playlist-end", fmt.Sprintf("%d", maxN+2),
		"--print", "%(webpage_url)s\t%(title).60s\t%(live_status)s\t%(duration)s",
		"--", hub,
	}
	cmd := exec.Command(bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	mobile := IsMobileSocialPlatform(platform)
	var items []LazyMediaItem
	seen := map[string]bool{primaryPage: true}
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		parts := strings.Split(ln, "\t")
		u := strings.TrimSpace(parts[0])
		if u == "" || seen[u] || !strings.HasPrefix(u, "http") {
			continue
		}
		seen[u] = true
		title := ""
		if len(parts) > 1 {
			title = strings.TrimSpace(parts[1])
		}
		kind := "vod"
		liveStatus := ""
		if len(parts) > 2 {
			liveStatus = strings.ToLower(strings.TrimSpace(parts[2]))
		}
		switch {
		case strings.Contains(liveStatus, "live") || liveStatus == "is_live":
			kind = "live"
		case strings.Contains(strings.ToLower(title), "clip"):
			kind = "clip"
		case mobile || strings.Contains(strings.ToLower(title), "short") || strings.Contains(strings.ToLower(title), "reel"):
			kind = "short"
		}
		// skip duplicate live primary-ish
		if kind == "live" && u == primaryPage {
			continue
		}
		items = append(items, LazyMediaItem{
			URL: u, Title: title, Kind: kind, Platform: platform, Mobile: mobile || kind == "short",
		})
		if len(items) >= maxN {
			break
		}
	}
	return items
}

// SocialLazyStagger is the delay between lazy secondary resolves.
func SocialLazyStagger() time.Duration {
	return 2800 * time.Millisecond
}

// FormatSocialStatus one-line for TUI status.
func FormatSocialStatus(r *ResolvedStream) string {
	if r == nil {
		return ""
	}
	live := "vod"
	if r.Live {
		live = "LIVE"
	}
	plat := r.Platform
	if plat == "" {
		plat = "social"
	}
	h := r.Handle
	if h == "" {
		return fmt.Sprintf("%s · %s", plat, live)
	}
	return fmt.Sprintf("%s/@%s · %s", plat, h, live)
}
