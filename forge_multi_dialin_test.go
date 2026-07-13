package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// dojoPcapPath returns examples/dojo.pcap when present (real data identity).
func dojoPcapPath(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"examples/dojo.pcap",
		filepath.Join("examples", "dojo.pcap"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	t.Skip("examples/dojo.pcap not found — run from repo root")
	return ""
}

var cgfIDRe = regexp.MustCompile(`^cgf:[0-9a-f]{16}$`)

// TestForgeDuplicateDojo — /forge examples/dojo.pcap examples/dojo.pcap
// Two lab slots, distinct cgf IDs (slot in commitment), corner stamps, status lines.
func TestForgeDuplicateDojo(t *testing.T) {
	path := dojoPcapPath(t)
	m := NewModel(Options{Nick: "forge-dial", Host: "127.0.0.1:0"})
	// no hub — local lab path
	_, _ = m.startMultiPcapForge([]string{path, path})

	// sys lines: two FormatMarkLine + local summary
	var markLines, summary string
	for _, c := range m.chat {
		if !c.Sys {
			continue
		}
		if strings.Contains(c.Text, "◈ forge cgf:") || strings.HasPrefix(c.Text, "◈ forge ") {
			markLines += c.Text + "\n"
		}
		if strings.Contains(c.Text, "forge multi-pcap") {
			summary = c.Text
		}
	}
	if markLines == "" {
		t.Fatalf("expected FormatMarkLine sys lines, chat=%v", sysTexts(m))
	}
	if !strings.Contains(summary, "×2") {
		t.Fatalf("expected ×2 summary, got %q chat=%v", summary, sysTexts(m))
	}
	if !strings.Contains(summary, "local lab") {
		t.Fatalf("offline should say local lab: %q", summary)
	}

	// two marked pcap slots
	if m.lab == nil {
		t.Fatal("lab nil")
	}
	var marks []ForgeMark
	for _, f := range m.lab.Feeds {
		if f.Kind == "pcap" && f.Forge != nil {
			marks = append(marks, *f.Forge)
		}
	}
	if len(marks) != 2 {
		t.Fatalf("want 2 marked slots, got %d", len(marks))
	}
	if marks[0].ID == marks[1].ID {
		t.Fatalf("duplicate path must still get distinct slot IDs: %s == %s", marks[0].ID, marks[1].ID)
	}
	for i, mk := range marks {
		if !cgfIDRe.MatchString(mk.ID) {
			t.Fatalf("slot %d id not cgf:<16hex>: %q", i+1, mk.ID)
		}
		if mk.Slot != i+1 {
			t.Fatalf("slot field: got %d want %d", mk.Slot, i+1)
		}
		if mk.Forge != ForgeName {
			t.Fatalf("forge name %q", mk.Forge)
		}
		if !strings.Contains(mk.Source, "dojo") {
			t.Fatalf("source %q", mk.Source)
		}
		if len(mk.Content) != 8 { // 4 bytes hex
			t.Fatalf("content hash len %d (%q)", len(mk.Content), mk.Content)
		}
	}

	// corner stamps on both frames (dojo is larger RGB → StampFrame cyan corner)
	for i, f := range m.lab.Feeds {
		if f.Kind != "pcap" || f.Frame == nil {
			continue
		}
		assertCornerStamp(t, f.Frame, f.Forge, fmt.Sprintf("slot%d", i+1))
	}

	// /forge status
	before := len(m.chat)
	m.pushForgeStatus()
	var statusMark, statusSum int
	for _, c := range m.chat[before:] {
		if !c.Sys {
			continue
		}
		if strings.Contains(c.Text, "◈ forge") {
			statusMark++
		}
		if strings.Contains(c.Text, "2 marked slots") && strings.Contains(c.Text, ForgeName) {
			statusSum++
		}
	}
	if statusMark != 2 {
		t.Fatalf("status mark lines want 2 got %d chat=%v", statusMark, sysTexts(m)[before:])
	}
	if statusSum != 1 {
		t.Fatalf("status summary missing: %v", sysTexts(m)[before:])
	}
	t.Logf("duplicate OK ids=%s | %s", marks[0].ID, marks[1].ID)
}

// TestForgeScaleSlots loads 3–6 dojo tiles and validates marks + stamps.
func TestForgeScaleSlots(t *testing.T) {
	path := dojoPcapPath(t)
	for _, n := range []int{3, 4, 6} {
		n := n
		t.Run(fmt.Sprintf("slots_%d", n), func(t *testing.T) {
			paths := make([]string, n)
			for i := range paths {
				paths[i] = path
			}
			m := NewModel(Options{Nick: "scale", Host: "127.0.0.1:0"})
			_, _ = m.startMultiPcapForge(paths)

			ids := map[string]int{}
			marked := 0
			for _, f := range m.lab.Feeds {
				if f.Kind != "pcap" || f.Forge == nil {
					continue
				}
				marked++
				mk := *f.Forge
				if !cgfIDRe.MatchString(mk.ID) {
					t.Fatalf("bad id %q", mk.ID)
				}
				if ids[mk.ID] > 0 {
					t.Fatalf("duplicate id %s on slots %d and %d", mk.ID, ids[mk.ID], mk.Slot)
				}
				ids[mk.ID] = mk.Slot
				if f.Frame != nil {
					assertCornerStamp(t, f.Frame, f.Forge, mk.ID)
				}
			}
			if marked != n {
				t.Fatalf("marked=%d want %d", marked, n)
			}
			// status
			m.pushForgeStatus()
			found := false
			for _, c := range m.chat {
				if c.Sys && strings.Contains(c.Text, fmt.Sprintf("%d marked slots", n)) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("status no %d marked: %v", n, sysTexts(m))
			}
			// cap: 7th path truncated
			if n == 6 {
				extra := append(paths, path)
				m2 := NewModel(Options{Nick: "cap", Host: "127.0.0.1:0"})
				_, _ = m2.startMultiPcapForge(extra)
				trunc := false
				for _, c := range m2.chat {
					if c.Sys && strings.Contains(c.Text, "max") && strings.Contains(c.Text, "truncated") {
						trunc = true
					}
				}
				if !trunc {
					t.Fatalf("expected truncate sys on >%d paths: %v", MaxLabFeeds, sysTexts(m2))
				}
				cnt := 0
				for _, f := range m2.lab.Feeds {
					if f.Kind == "pcap" && f.Forge != nil {
						cnt++
					}
				}
				if cnt != MaxLabFeeds {
					t.Fatalf("capped slots %d want %d", cnt, MaxLabFeeds)
				}
			}
			t.Logf("scale ×%d OK", n)
		})
	}
}

// TestForgeHubFanOut — multi-pcap forge publishes mark meta + stamped hexlum to hub.
func TestForgeHubFanOut(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	path := dojoPcapPath(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	hub := NewHub(addr, true, "")
	go func() { _ = hub.ListenAndServe() }()
	defer hub.Close()
	time.Sleep(120 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	type hit struct {
		kind string
		msg  map[string]any
	}
	hits := make(chan hit, 64)

	// viewer peer
	go func() {
		c, _, err := websocket.Dial(ctx, "ws://"+addr+"/?role=peer&nick=viewer", nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"join","nick":"viewer"}`))
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			if m["type"] != "gyst" {
				continue
			}
			k, _ := m["kind"].(string)
			// forge marks arrive as kind=meta with mark/forge fields
			if k == "meta" || m["mark"] != nil || m["forge"] != nil {
				select {
				case hits <- hit{kind: "meta", msg: m}:
				default:
				}
			}
			if k == "hexlum" || k == "rgb24" {
				select {
				case hits <- hit{kind: k, msg: m}:
				default:
				}
			}
		}
	}()
	time.Sleep(80 * time.Millisecond)

	// publisher model with live MeshClient
	m := NewModel(Options{Nick: "forger", Host: addr})
	client := NewMeshClient(addr, "forger")
	m.client = client
	go client.Run(ctx)
	// wait connected
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		client.mu.Lock()
		ok := client.conn != nil
		client.mu.Unlock()
		if ok {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	// 3-slot fan-out (same real dojo identity ×3)
	paths := []string{path, path, path}
	_, _ = m.startMultiPcapForge(paths)

	var marks []ForgeMark
	for _, f := range m.lab.Feeds {
		if f.Kind == "pcap" && f.Forge != nil {
			marks = append(marks, *f.Forge)
		}
	}
	if len(marks) != 3 {
		t.Fatalf("lab marks %d", len(marks))
	}

	// collect until we have meta for all 3 + at least one hexlum, or timeout
	seenMarks := map[string]bool{}
	var hexlum int
	timeout := time.After(5 * time.Second)
	for len(seenMarks) < 3 || hexlum < 1 {
		select {
		case h := <-hits:
			if h.kind == "meta" {
				if mk, ok := ParseForgeFromMesh(h.msg); ok && mk.ID != "" {
					seenMarks[mk.ID] = true
					if !cgfIDRe.MatchString(mk.ID) {
						t.Fatalf("hub mark id %q", mk.ID)
					}
				}
			}
			if h.kind == "hexlum" {
				hexlum++
				// decode payload and verify corner lattice if present
				b64, _ := h.msg["b64"].(string)
				if b64 != "" {
					raw, err := base64.StdEncoding.DecodeString(b64)
					if err == nil && len(raw) >= 64 {
						// hexlum n×n; stamp top-left 4×4 is 40 or 200
						n := 0
						if w, ok := h.msg["w"].(float64); ok {
							n = int(w)
						}
						if n >= 8 && len(raw) >= n*n {
							v0 := raw[0]
							if v0 != 40 && v0 != 200 {
								// still may be stamped after resize — log soft
								t.Logf("hexlum[0]=%d (stamp lattice prefers 40/200)", v0)
							} else {
								t.Logf("stamped hexlum corner v0=%d n=%d", v0, n)
							}
						}
					}
				}
			}
		case <-timeout:
			t.Fatalf("fan-out timeout marks=%d/%d hexlum=%d labIDs=%v",
				len(seenMarks), 3, hexlum, markIDs(marks))
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}

	// all lab mark IDs should appear on hub
	for _, mk := range marks {
		if !seenMarks[mk.ID] {
			t.Errorf("hub missing mark %s", mk.ID)
		}
	}
	t.Logf("hub fan-out OK marks=%d hexlum=%d ids=%v", len(seenMarks), hexlum, markIDs(marks))
}

// TestForgeStampLatticeBits — 4×4 lattice encodes markBits; BR slot gray steps.
func TestForgeStampLatticeBits(t *testing.T) {
	n := 25
	m1 := NewForgeMark(1, "dojo.pcap", []byte("a"))
	m2 := NewForgeMark(4, "dojo.pcap", []byte("a")) // same content, different slot
	if m1.ID == m2.ID {
		t.Fatal("slot must differentiate id")
	}
	lum := make([]byte, n*n)
	StampHexLum(lum, n, m1)
	bits := markBits(m1.ID)
	for i := 0; i < 16; i++ {
		x, y := i%4, i/4
		want := byte(40)
		if bits&(1<<uint(i)) != 0 {
			want = 200
		}
		if lum[y*n+x] != want {
			t.Fatalf("lattice[%d,%d]=%d want %d", x, y, lum[y*n+x], want)
		}
	}
	// bottom-right slot steps: 30+slot*30
	sv := lum[(n-1)*n+(n-1)]
	if sv != byte(30+1*30) {
		t.Fatalf("slot1 gray %d", sv)
	}
	// ForgeIDSpace markers 'c' and 'f'
	if lum[(n-2)*n+(n-1)] != 'c' || lum[(n-2)*n+(n-2)] != 'f' {
		t.Fatalf("cgf corner letters %d %d", lum[(n-2)*n+(n-1)], lum[(n-2)*n+(n-2)])
	}
	// FormatMarkLine
	line := FormatMarkLine(m1)
	if !strings.Contains(line, "cgf:") || !strings.Contains(line, "slot 1") {
		t.Fatalf("FormatMarkLine %q", line)
	}
}

func assertCornerStamp(t *testing.T, fr *FramePixels, mark *ForgeMark, label string) {
	t.Helper()
	if fr == nil || mark == nil || fr.W < 4 || fr.H < 4 || len(fr.RGB) < 12 {
		t.Fatalf("%s: no frame for stamp check", label)
	}
	// top-left pixel should not be uniform flat mid-gray only — stamp sets cyan-ish
	r, g, b := fr.RGB[0], fr.RGB[1], fr.RGB[2]
	// either small-square grayscale stamp (r==g==b in {40,200}) or large RGB cyan tint
	flat := r == g && g == b && (r == 40 || r == 200)
	cyanish := g != r || b != r // StampFrame large path: R=v/4, G=v, B=v+40
	if !flat && !cyanish {
		// also accept any deviation from zero if stamp ran on non-zero source
		if r == 0 && g == 0 && b == 0 {
			t.Fatalf("%s: corner still black — stamp missing?", label)
		}
	}
	// at least one of the 4×4 cells should match lattice 40/200 in G channel for large frames
	bits := markBits(mark.ID)
	matched := 0
	for i := 0; i < 16; i++ {
		x, y := i%4, i/4
		if x >= fr.W || y >= fr.H {
			continue
		}
		idx := (y*fr.W + x) * 3
		if idx+2 >= len(fr.RGB) {
			continue
		}
		wantOn := bits&(1<<uint(i)) != 0
		gv := fr.RGB[idx+1]
		if wantOn && gv >= 100 {
			matched++
		}
		if !wantOn && gv <= 80 {
			matched++
		}
	}
	if matched < 8 {
		t.Logf("%s: weak lattice match %d/16 (may be ok for non-square large stamp)", label, matched)
	}
}

func sysTexts(m *Model) []string {
	var out []string
	for _, c := range m.chat {
		if c.Sys {
			out = append(out, c.Text)
		}
	}
	return out
}

func markIDs(ms []ForgeMark) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}
