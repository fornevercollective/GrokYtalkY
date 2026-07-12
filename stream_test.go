package main

import "testing"

func TestNeedsYtDlp(t *testing.T) {
	yes := []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ",
		"https://twitch.tv/somechannel",
		"https://x.com/user/status/123",
	}
	for _, u := range yes {
		if !needsYtDlp(u) {
			t.Fatalf("expected yt-dlp for %s", u)
		}
	}
	no := []string{
		"https://cdn.example.com/vid.mp4",
		"https://ex.com/live/playlist.m3u8",
		"rtsp://cam.local/stream",
	}
	for _, u := range no {
		if needsYtDlp(u) {
			t.Fatalf("did not expect yt-dlp for %s", u)
		}
	}
}

func TestIsDirectMediaURL(t *testing.T) {
	if !isDirectMediaURL("https://a.com/x.m3u8?token=1") {
		t.Fatal("m3u8")
	}
	if isDirectMediaURL("https://youtube.com/watch?v=1") {
		t.Fatal("youtube page is not direct")
	}
}

func TestResolveLocalMissing(t *testing.T) {
	_, err := ResolveMedia("/no/such/video.mp4")
	if err == nil {
		t.Fatal("expected error")
	}
}
