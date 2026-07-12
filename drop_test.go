package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestParseDroppedPathsQuoted(t *testing.T) {
	paths := ParseDroppedPaths(`'/Users/me/My Video/clip.mp4'`)
	if len(paths) != 1 || paths[0] != "/Users/me/My Video/clip.mp4" {
		t.Fatalf("%v", paths)
	}
	paths = ParseDroppedPaths(`/a/b.mp4 /c/d.mov`)
	if len(paths) != 2 {
		t.Fatal(paths)
	}
	paths = ParseDroppedPaths(`file:///Users/me/pic.png`)
	if len(paths) != 1 || paths[0] != "/Users/me/pic.png" {
		t.Fatalf("%v", paths)
	}
	paths = ParseDroppedPaths("/Users/me/My\\ Video/x.webm")
	if len(paths) != 1 || paths[0] != "/Users/me/My Video/x.webm" {
		t.Fatalf("%v", paths)
	}
}

func TestLooksLikeDropPaste(t *testing.T) {
	if !LooksLikeDropPaste("https://youtu.be/abc") {
		t.Fatal("url")
	}
	if !LooksLikeDropPaste("/tmp/foo.mp4") {
		t.Fatal("path")
	}
	if LooksLikeDropPaste("hello there how are you") {
		t.Fatal("prose")
	}
}

func TestLoadImageFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.jpg")
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	fp, err := LoadImageFile(p, 32, 32)
	if err != nil {
		t.Fatal(err)
	}
	if fp.W < 1 || fp.H < 1 {
		t.Fatal(fp.W, fp.H)
	}
}

func TestIsImageVideo(t *testing.T) {
	if !IsImagePath("a.PNG") {
		t.Fatal("png")
	}
	if !IsMediaPath("b.mkv") {
		t.Fatal("mkv")
	}
}

func TestMultiLineDrop(t *testing.T) {
	s := "/tmp/a.mp4\n/tmp/b.png\n"
	paths := ParseDroppedPaths(s)
	if len(paths) != 2 {
		t.Fatal(paths)
	}
}
