package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Drag-and-drop / path paste helpers.
// Terminal.app & iTerm2 insert file paths (often quoted or file://) on drop.
// Bracketed paste arrives as tea.PasteMsg.

// imageExts still frames we load as RGB tiles (no audio pipe).
var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".webp": true, ".bmp": true, ".tif": true, ".tiff": true,
	".heic": true, ".heif": true,
}

// videoExts containers / streams opened via ffmpeg watch.
var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".mov": true, ".webm": true,
	".avi": true, ".m4v": true, ".ts": true, ".m3u8": true,
	".flv": true, ".wmv": true, ".ogv": true, ".mpeg": true,
	".mpg": true, ".mpd": true, ".gif": true, // gif can be either; prefer video pipe if animated later
}

// ParseDroppedPaths extracts filesystem paths / URLs from a paste or drop string.
func ParseDroppedPaths(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// normalize newlines → spaces for multi-line Finder drops
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	// if every line is a path, treat as multi-drop
	if strings.Contains(s, "\n") {
		var out []string
		for _, ln := range strings.Split(s, "\n") {
			ln = strings.TrimSpace(ln)
			if ln == "" {
				continue
			}
			out = append(out, ParseDroppedPaths(ln)...)
		}
		return uniquePaths(out)
	}

	var out []string
	// tokenize with quotes + backslash escapes (Terminal/iTerm style)
	var cur strings.Builder
	inSingle, inDouble, esc := false, false, false
	flush := func() {
		p := strings.TrimSpace(cur.String())
		cur.Reset()
		if p == "" {
			return
		}
		if n := normalizeDropPath(p); n != "" {
			out = append(out, n)
		}
	}
	for _, r := range s {
		if esc {
			cur.WriteRune(r)
			esc = false
			continue
		}
		if r == '\\' && !inSingle {
			esc = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if !inSingle && !inDouble && unicode.IsSpace(r) {
			flush()
			continue
		}
		cur.WriteRune(r)
	}
	flush()
	return uniquePaths(out)
}

func normalizeDropPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, `"'`)
	if p == "" {
		return ""
	}
	// file:// URL
	if strings.HasPrefix(strings.ToLower(p), "file:") {
		u, err := url.Parse(p)
		if err == nil {
			if u.Path != "" {
				p = u.Path
			}
			// file://localhost/Users/...
			if u.Host != "" && u.Host != "localhost" && !strings.HasPrefix(p, "/") {
				p = "/" + u.Host + p
			}
		}
		// percent-decode
		if d, err := url.PathUnescape(p); err == nil {
			p = d
		}
	}
	// expand ~
	p = expandPath(p)
	// reject pure URLs that aren't file — keep http for watch
	if isURL(p) {
		return p
	}
	// must look like a path
	if !strings.Contains(p, "/") && !strings.Contains(p, `\`) {
		// bare filename — allow if exists in cwd
		if _, err := os.Stat(p); err != nil {
			return ""
		}
	}
	return p
}

func uniquePaths(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// IsImagePath true for still images we can decode without ffmpeg.
func IsImagePath(p string) bool {
	ext := strings.ToLower(filepath.Ext(stripQuery(p)))
	return imageExts[ext]
}

// IsMediaPath image or video/stream for drop handling.
func IsMediaPath(p string) bool {
	if isURL(p) || isRawStreamURL(p) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(stripQuery(p)))
	if imageExts[ext] || videoExts[ext] {
		return true
	}
	// binary stream codecs
	switch ext {
	case ".gyst", ".gyhex", ".gybin", ".pcap", ".hex":
		return true
	}
	// existing file with no known ext — still try (ffmpeg may open it)
	if !isURL(p) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

func stripQuery(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		return p[:i]
	}
	return p
}

// LooksLikeDropPaste true when paste is mostly paths/URLs (not chat prose).
func LooksLikeDropPaste(s string) bool {
	paths := ParseDroppedPaths(s)
	if len(paths) == 0 {
		return false
	}
	// single path/url
	if len(paths) == 1 && IsMediaPath(paths[0]) {
		return true
	}
	// multi: majority media
	media := 0
	for _, p := range paths {
		if IsMediaPath(p) {
			media++
		}
	}
	return media > 0 && media*2 >= len(paths)
}

// LoadImageFile decodes a still image into FramePixels (for lab tiles / cam strip).
func LoadImageFile(path string, maxW, maxH int) (*FramePixels, error) {
	path = expandPath(path)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeFrameJPEG(b, maxW, maxH) // image.Decode handles jpeg/png via imports
}
