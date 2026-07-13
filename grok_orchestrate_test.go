package main

import (
	"strings"
	"testing"
)

func TestParseGrokTake(t *testing.T) {
	raw := `STYLE neon
CAPTION Markets open higher on tech
PATTERN s("bd*4, ~ sd")
GLYPH phone-v
EFFECT scanline rain
NOTE try dual burst
`
	take := ParseGrokTake(raw)
	if take.Style != "neon" || take.Caption == "" || take.Pattern == "" {
		t.Fatalf("%+v", take)
	}
	if take.Glyph != "phone-v" || take.Effect == "" {
		t.Fatalf("%+v", take)
	}
	if !strings.Contains(take.TakeSummary(), "style=neon") {
		t.Fatal(take.TakeSummary())
	}
}

func TestParsePixelStyleName(t *testing.T) {
	m, ok := ParsePixelStyleName("matrix")
	if !ok || m != PixelHalf {
		t.Fatal(m, ok)
	}
	m, ok = ParsePixelStyleName("neon")
	if !ok || m != PixelNeon {
		t.Fatal(m)
	}
	_, ok = ParsePixelStyleName("not-a-style")
	if ok {
		t.Fatal("expected false")
	}
}

func TestBuildOrchestrateUserPrompt(t *testing.T) {
	u := BuildOrchestrateUserPrompt(FeedOrchestrateContext{
		Mode: "news", Active: "Al Jazeera", Kind: "news",
		Style: "hex", GlyphN: 25, GlyphAsp: "square", Live: true,
		NewsCount: 6, Media: "media 3/16", Hint: "tense geopolitics",
	})
	if !strings.Contains(u, "Al Jazeera") || !strings.Contains(u, "news_tiles=6") {
		t.Fatal(u)
	}
}

func TestParseGrokTakeProseCaption(t *testing.T) {
	take := ParseGrokTake("Breaking: storm approaches coast")
	if take.Caption == "" {
		t.Fatal("want caption fallback")
	}
}
