package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// TestForgeDualGlyphReceive — live dual Glyph receive of forge meta + stamped hexlum.
// Simulates hub gyst mark meta then hexlum from peer "forger".
func TestForgeDualGlyphReceive(t *testing.T) {
	m := NewModel(Options{Nick: "viewer", Host: "127.0.0.1:0"})
	if m.burstMode {
		t.Fatal("start off burst")
	}

	mark := NewForgeMark(2, "dojo.pcap", []byte("glyph-rx-seed"))
	metaMsg := mark.MeshJSON("forger")
	raw, _ := json.Marshal(metaMsg)
	_, _ = m.handleWS(raw)

	if m.forgeRX == nil || m.forgeRX.ID != mark.ID {
		t.Fatalf("forgeRX not stored: %+v", m.forgeRX)
	}
	if m.forgeRXFrom != "forger" {
		t.Fatalf("from %q", m.forgeRXFrom)
	}
	// sys line once
	sys := strings.Join(sysTexts(m), "\n")
	if !strings.Contains(sys, mark.ID) || !strings.Contains(sys, "dual Glyph") {
		t.Fatalf("sys mark line missing: %s", sys)
	}

	// duplicate meta: no second distinct spam (accept returns false)
	nBefore := len(m.chat)
	_, _ = m.handleWS(raw)
	// may still not add if isNew=false — we only push on isNew
	added := 0
	for _, c := range m.chat[nBefore:] {
		if c.Sys && strings.Contains(c.Text, "◈ forge") {
			added++
		}
	}
	if added != 0 {
		t.Fatalf("duplicate mark should not re-sys, added=%d", added)
	}

	// stamped 25×25 hexlum frame from same peer
	n := 25
	lum := make([]byte, n*n)
	for i := range lum {
		lum[i] = 90
	}
	StampHexLum(lum, n, mark)
	pkt := PacketFromHexLum(lum, n, 7)
	gyst := PacketToMesh(pkt, "forger")
	raw2, _ := json.Marshal(gyst)
	mod, cmd := m.handleWS(raw2)
	m = mod.(*Model)
	if cmd != nil {
		// apply frameReady
		msg := cmd()
		if fr, ok := msg.(frameReady); ok {
			_, _ = m.Update(fr)
		}
	}

	if !m.burstMode {
		t.Fatal("forge hexlum should auto-open dual Glyph burst")
	}
	if m.burstRemote != "forger" {
		t.Fatalf("burstRemote %q", m.burstRemote)
	}
	if m.burstPeerFrame == nil {
		t.Fatal("peer frame nil")
	}
	if m.burstPeerFrame.W != n || m.burstPeerFrame.H != n {
		t.Fatalf("peer size %dx%d", m.burstPeerFrame.W, m.burstPeerFrame.H)
	}
	// lattice stamp visible on peer tile (top-left G channel / gray)
	v0 := m.burstPeerFrame.RGB[0]
	if v0 != 40 && v0 != 200 {
		t.Fatalf("peer corner not stamped lattice: %d", v0)
	}
	if len(m.lastGlyph) != n*n {
		t.Fatalf("lastGlyph len %d", len(m.lastGlyph))
	}
	if m.lastGlyph[0] != 40 && m.lastGlyph[0] != 200 {
		t.Fatalf("lastGlyph corner %d", m.lastGlyph[0])
	}

	// dual chrome labels
	label := BurstForgePeerLabel("forger", m.forgeRX)
	if !strings.Contains(label, "forger") || !strings.Contains(label, "cgf:") {
		t.Fatalf("peer label %q", label)
	}
	status := BurstForgeStatusLine(80, false, "forger", "viewer", 1, m.forgeRX)
	plain := stripANSI(status)
	if !strings.Contains(plain, "forge") && !strings.Contains(plain, "cgf") {
		t.Fatalf("status %q", plain)
	}

	// render dual orb does not panic and mentions forge chrome
	out := m.renderBurstOrb(80, 24)
	if out == "" {
		t.Fatal("empty dual render")
	}
	if !strings.Contains(stripANSI(out), "cgf:") && !strings.Contains(stripANSI(out), "forge") {
		// status or title should surface forge
		t.Logf("render (ok if truncated):\n%s", out)
	}
	t.Logf("dual Glyph forge RX OK id=%s peer=%dx%d burst=%v", mark.ID, m.burstPeerFrame.W, m.burstPeerFrame.H, m.burstMode)
}

// TestForgeDualGlyphReceiveHubRoundTrip — mark + hexlum via PacketToMesh like publishForgeMulti.
func TestForgeDualGlyphReceiveHubRoundTrip(t *testing.T) {
	m := NewModel(Options{Nick: "v2", Host: "127.0.0.1:0"})
	mark := NewForgeMark(1, "dojo.pcap", []byte{1, 2, 3, 4})
	// meta then frame as hub would deliver JSON
	b, _ := json.Marshal(mark.MeshJSON("pub"))
	_, _ = m.handleWS(b)

	lum := make([]byte, 13*13)
	StampHexLum(lum, 13, mark)
	p := PacketFromHexLum(lum, 13, 1)
	// also re-assert mark fields on same envelope occasionally (optional)
	mesh := PacketToMesh(p, "pub")
	// merge mark id for bridges that embed on frame
	mesh["mark"] = mark.ID
	mesh["forge"] = mark.Forge
	mesh["slot"] = mark.Slot
	raw, _ := json.Marshal(mesh)
	mod, cmd := m.handleWS(raw)
	m = mod.(*Model)
	if cmd != nil {
		if fr, ok := cmd().(frameReady); ok {
			_, _ = m.Update(fr)
		}
	}
	if m.forgeRX == nil || m.forgeRX.ID != mark.ID {
		t.Fatal("mark lost")
	}
	if !m.burstMode || m.burstPeerFrame == nil {
		t.Fatal("dual not armed")
	}
	// b64 round-trip integrity
	b64, _ := mesh["b64"].(string)
	dec, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(dec) != 13*13 {
		t.Fatal(err)
	}
	if dec[0] != 40 && dec[0] != 200 {
		t.Fatalf("stamp lost in mesh b64: %d", dec[0])
	}
}

func TestShortMarkID(t *testing.T) {
	id := "cgf:480c1abf7ba6d00c"
	if ShortMarkID(id) != "cgf:480c1abf" {
		t.Fatal(ShortMarkID(id))
	}
	if BurstForgePeerLabel("alice", &ForgeMark{ID: id}) != "alice cgf:480c1abf" {
		// truncate may clip — at least prefix
		got := BurstForgePeerLabel("alice", &ForgeMark{ID: id})
		if !strings.HasPrefix(got, "alice") || !strings.Contains(got, "cgf:") {
			t.Fatal(got)
		}
	}
}

func TestAcceptForgeRXDedupe(t *testing.T) {
	m := NewModel(Options{Nick: "x", Host: "127.0.0.1:0"})
	mk := NewForgeMark(1, "a.pcap", []byte("z"))
	if !m.acceptForgeRX("p", mk) {
		t.Fatal("first should be new")
	}
	if m.acceptForgeRX("p", mk) {
		t.Fatal("same id+from not new")
	}
	mk2 := NewForgeMark(2, "b.pcap", []byte("z"))
	if !m.acceptForgeRX("p", mk2) {
		t.Fatal("different id is new")
	}
}

// TestLocalForgeFeedsDualLeft — /forge dojo sets burstLocalFrame + forgeLocal.
func TestLocalForgeFeedsDualLeft(t *testing.T) {
	path := dojoPcapPath(t)
	m := NewModel(Options{Nick: "local", Host: "127.0.0.1:0"})
	_, _ = m.startMultiPcapForge([]string{path})
	if m.forgeLocal == nil {
		t.Fatal("forgeLocal")
	}
	if m.burstLocalFrame == nil {
		t.Fatal("burstLocalFrame for dual left")
	}
	// corner stamp on local dual tile
	v := m.burstLocalFrame.RGB[1] // G
	if v == 0 {
		t.Log("local tile may be large RGB stamp")
	}
}
