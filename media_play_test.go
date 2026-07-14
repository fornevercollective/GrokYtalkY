package main

import (
	"strings"
	"testing"
)

func TestRewriteM3u8RelativeAndAbsolute(t *testing.T) {
	id := MediaPlayRegister("https://example.com/live", "https://cdn.example.com/live/master.m3u8")
	body := `#EXTM3U
#EXT-X-VERSION:3
chunk0.ts
https://cdn.example.com/live/chunk1.ts
#EXT-X-KEY:METHOD=AES-128,URI="key.bin"
`
	out := rewriteM3u8(body, "https://cdn.example.com/live/master.m3u8", id, "http://127.0.0.1:9876")
	if !strings.Contains(out, "/api/media/proxy?u=") {
		t.Fatalf("expected proxy rewrite, got:\n%s", out)
	}
	// relative chunk0.ts should become absolute then proxied
	if !strings.Contains(out, "cdn.example.com") {
		t.Fatalf("relative chunk not rewritten:\n%s", out)
	}
	if !strings.Contains(out, "URI=\"http://127.0.0.1:9876/api/media/proxy") {
		t.Fatalf("EXT-X-KEY URI not rewritten:\n%s", out)
	}
	// allowed set should include absolute URLs
	if _, ok := mediaPlayAllowed("https://cdn.example.com/live/chunk0.ts"); !ok {
		t.Fatal("chunk0 not allowed after rewrite")
	}
}

func TestWrapResolvedForBrowserHLS(t *testing.T) {
	src := &ResolvedStream{
		Video: "https://manifest.googlevideo.com/api/manifest/hls_playlist/x.m3u8",
		Via:   "yt-dlp",
		Live:  true,
	}
	video, via, kind := WrapResolvedForBrowser("https://www.youtube.com/@CNN/live", src, "http://192.168.0.104:9876")
	if kind != "hls" {
		t.Fatalf("kind=%s", kind)
	}
	if !strings.Contains(video, "http://192.168.0.104:9876/api/media/play/") {
		t.Fatalf("video=%s", video)
	}
	if !strings.Contains(via, "hub-proxy") {
		t.Fatalf("via=%s", via)
	}
}

func TestIsHLSLikeURL(t *testing.T) {
	if !isHLSLikeURL("https://x.com/a.m3u8?token=1") {
		t.Fatal("m3u8")
	}
	if !isHLSLikeURL("https://manifest.googlevideo.com/api/manifest/hls_playlist/x") {
		t.Fatal("googlevideo")
	}
}
