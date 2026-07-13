package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseSocialQuery(t *testing.T) {
	cases := []struct {
		in, plat, handle string
	}{
		{"@shroud", "", "shroud"},
		{"twitch:shroud", SocialTwitch, "shroud"},
		{"twitch/shroud", SocialTwitch, "shroud"},
		{"yt:@mkbhd", SocialYouTube, "mkbhd"},
		{"youtube:@veritasium", SocialYouTube, "veritasium"},
		{"tt:@charlidamelio", SocialTikTok, "charlidamelio"},
		{"tiktok:user1", SocialTikTok, "user1"},
		{"kick:xqc", SocialKick, "xqc"},
		{"x:@elonmusk", SocialX, "elonmusk"},
		{"ig:@natgeo", SocialInstagram, "natgeo"},
		{"social:@foo", "", "foo"},
		{"handle:twitch:bar", SocialTwitch, "bar"},
	}
	for _, c := range cases {
		q := ParseSocialQuery(c.in)
		if q == nil {
			t.Fatalf("%q: nil", c.in)
		}
		if q.Platform != c.plat || q.Handle != c.handle {
			t.Fatalf("%q: got %s/%s want %s/%s", c.in, q.Platform, q.Handle, c.plat, c.handle)
		}
	}
	// not social
	for _, no := range []string{
		"", "movie.mp4", "https://youtube.com/watch?v=1", "/tmp/a.mkv", "rtsp://cam",
	} {
		if ParseSocialQuery(no) != nil {
			t.Fatalf("expected nil for %q", no)
		}
	}
}

func TestSocialLiveCandidatesOrder(t *testing.T) {
	yt := socialLiveCandidates(SocialYouTube, "mkbhd")
	if len(yt) < 2 || !strings.Contains(yt[0], "/live") {
		t.Fatalf("youtube live first: %v", yt)
	}
	tt := socialLiveCandidates(SocialTikTok, "a")
	if len(tt) < 1 || !strings.Contains(tt[0], "/live") {
		t.Fatalf("tiktok live first: %v", tt)
	}
	tw := socialLiveCandidates(SocialTwitch, "shroud")
	if len(tw) < 1 || !strings.Contains(tw[0], "twitch.tv/shroud") {
		t.Fatalf("twitch: %v", tw)
	}
}

func TestMobileGlyphStackDouble(t *testing.T) {
	w, h := MobileGlyphStackSize(25)
	if w != 100 || h != 200 {
		t.Fatalf("25 double stack %dx%d", w, h)
	}
	// height is 2× width (portrait double stack)
	if h != w*2 {
		t.Fatalf("want 1:2 aspect, got %dx%d", w, h)
	}
	half := MobilePortraitHalfRows(40, 25)
	if half < 6 {
		t.Fatal(half)
	}
	if !IsMobileSocialPlatform(SocialTikTok) || IsMobileSocialPlatform(SocialTwitch) {
		t.Fatal("mobile flags")
	}
}

func TestResolveMediaSocialSyntax(t *testing.T) {
	// Parse path only — full yt-dlp resolve needs network; ensure social branch is taken
	// by missing-yt or timeout-safe unit: empty handle rejected
	_, err := ResolveSocial(&SocialQuery{Handle: ""})
	if err == nil {
		t.Fatal("empty handle")
	}
}

func TestIsLivePageURL(t *testing.T) {
	if !isLivePageURL("https://www.twitch.tv/foo") {
		t.Fatal("twitch")
	}
	if !isLivePageURL("https://youtube.com/@x/live") {
		t.Fatal("yt live")
	}
	if isLivePageURL("https://cdn.example.com/v.mp4") {
		t.Fatal("mp4 not live page")
	}
}

func TestFormatSocialStatus(t *testing.T) {
	s := FormatSocialStatus(&ResolvedStream{
		Live: true, Platform: SocialTwitch, Handle: "shroud",
	})
	if !strings.Contains(s, "LIVE") || !strings.Contains(s, "shroud") {
		t.Fatal(s)
	}
}

func TestGenSocialPoster(t *testing.T) {
	f := genSocialPoster(32, 40, "clip", "live", SocialTwitch)
	if f == nil || f.W != 32 || len(f.RGB) != 32*40*3 {
		t.Fatal("poster")
	}
}

func TestSocialLazyStagger(t *testing.T) {
	if SocialLazyStagger() < 500*time.Millisecond {
		t.Fatal("stagger too short")
	}
}
