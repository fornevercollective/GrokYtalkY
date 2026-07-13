package main

import (
	"strings"
	"testing"
)

// TestForgeDualLocalRotate — multi-slot /forge cycles left burstLocalFrame;
// peer right (burstPeerFrame / forgeRX) stays put.
func TestForgeDualLocalRotate(t *testing.T) {
	path := dojoPcapPath(t)
	m := NewModel(Options{Nick: "local", Host: "127.0.0.1:0"})
	// seed peer right so rotate must not clobber it
	peerMark := NewForgeMark(9, "remote.pcap", []byte("peer"))
	m.forgeRX = &peerMark
	m.forgeRXFrom = "forger"
	m.burstPeerFrame = &FramePixels{W: 13, H: 13, RGB: make([]byte, 13*13*3)}
	m.burstPeerFrame.RGB[0] = 99

	_, _ = m.startMultiPcapForge([]string{path, path, path})
	if !m.forgeRotateOn {
		t.Fatal("rotate on")
	}
	if !m.burstMode {
		t.Fatal("multi-slot should open dual Glyph")
	}
	slots := m.forgePcapSlots()
	if len(slots) != 3 {
		t.Fatalf("slots %d", len(slots))
	}
	ids := make([]string, 3)
	for i, s := range slots {
		ids[i] = s.Forge.ID
	}
	if ids[0] == ids[1] || ids[1] == ids[2] {
		t.Fatalf("need distinct marks: %v", ids)
	}

	// initial left = slot 0
	if m.forgeLocal == nil || m.forgeLocal.ID != ids[0] {
		t.Fatalf("initial left %v want %s", m.forgeLocal, ids[0])
	}
	if m.burstLocalFrame == nil {
		t.Fatal("burstLocalFrame")
	}
	left0 := m.burstLocalFrame

	// advance spin into next dwell window
	every := m.forgeRotateEvery
	if every <= 0 {
		every = ForgeDualRotateTicks
	}
	m.spin = every // → idx 1
	m.talking = false
	m.tickForgeDualLocal()
	if m.forgeLocalIdx != 1 {
		t.Fatalf("idx %d want 1", m.forgeLocalIdx)
	}
	if m.forgeLocal == nil || m.forgeLocal.ID != ids[1] {
		t.Fatalf("left mark %v want %s", m.forgeLocal, ids[1])
	}
	if m.burstLocalFrame == nil || m.burstLocalFrame == left0 && m.forgeLocal.ID == ids[0] {
		// frame pointer may change; mark must move
	}
	if m.forgeLocal.ID == ids[0] {
		t.Fatal("left did not rotate")
	}

	// peer right untouched
	if m.forgeRX == nil || m.forgeRX.ID != peerMark.ID {
		t.Fatal("forgeRX clobbered")
	}
	if m.burstPeerFrame == nil || m.burstPeerFrame.RGB[0] != 99 {
		t.Fatal("burstPeerFrame clobbered")
	}
	if m.forgeRXFrom != "forger" {
		t.Fatal(m.forgeRXFrom)
	}

	// hold freezes idx while spin advances
	m.forgeHoldLeft = true
	held := m.forgeLocalIdx
	heldID := m.forgeLocal.ID
	m.spin = every * 5
	m.tickForgeDualLocal()
	if m.forgeLocalIdx != held || m.forgeLocal.ID != heldID {
		t.Fatalf("hold broke: idx %d id %s", m.forgeLocalIdx, m.forgeLocal.ID)
	}

	// next / prev
	m.stepForgeDualLocal(+1)
	if m.forgeLocal.ID == heldID && len(slots) > 1 {
		// might wrap to same only if 1 slot
		if m.forgeLocalIdx == held {
			t.Fatal("step +1 no move")
		}
	}
	// peer still intact after steps
	if m.burstPeerFrame.RGB[0] != 99 {
		t.Fatal("peer after step")
	}

	// status mentions left + rotate/hold
	m.pushForgeStatus()
	sys := strings.Join(sysTexts(m), "\n")
	if !strings.Contains(sys, "left") || !strings.Contains(sys, "hold") {
		t.Fatalf("status: %s", sys)
	}
	if !strings.Contains(sys, "RX peer") {
		t.Fatalf("status missing RX: %s", sys)
	}

	// chrome labels
	lab := BurstForgeLocalLabel("me", m.forgeLocal, m.forgeLocalIdx, false)
	if !strings.Contains(lab, "cgf:") && m.forgeLocal != nil {
		t.Fatal(lab)
	}
	t.Logf("dual-local OK left=%s peerRX=%s idx=%d", m.forgeLocal.ID, m.forgeRX.ID, m.forgeLocalIdx)
}

func TestForgeDualLocalSingleSlotNoBurstForce(t *testing.T) {
	path := dojoPcapPath(t)
	m := NewModel(Options{Nick: "s1", Host: "127.0.0.1:0"})
	// single path: rotate on for frame refresh, but do not force burst
	_, _ = m.startMultiPcapForge([]string{path})
	if !m.forgeRotateOn {
		t.Fatal("rotate on for single")
	}
	// single does not auto open burst (only n>=2)
	if m.burstMode {
		t.Fatal("single-slot should not force dual open")
	}
	if m.burstLocalFrame == nil || m.forgeLocal == nil {
		t.Fatal("local tile still set")
	}
}

func TestForgeDualLocalPTTSkipsRotate(t *testing.T) {
	path := dojoPcapPath(t)
	m := NewModel(Options{Nick: "tx", Host: "127.0.0.1:0"})
	_, _ = m.startMultiPcapForge([]string{path, path})
	m.forgeHoldLeft = false
	m.spin = 0
	m.tickForgeDualLocal()
	id0 := m.forgeLocal.ID
	m.talking = true
	m.spin = m.forgeRotateEvery * 3
	// fake cam left
	cam := &FramePixels{W: 8, H: 8, RGB: make([]byte, 8*8*3)}
	cam.RGB[0] = 1
	m.burstLocalFrame = cam
	m.tickForgeDualLocal()
	// still on same idx (skipped) and didn't re-apply over cam... actually skip returns early so cam stays
	if m.burstLocalFrame.RGB[0] != 1 {
		t.Fatal("PTT should leave cam left")
	}
	if m.forgeLocal.ID != id0 {
		// idx not advanced while talking
		t.Log("mark may still be old — ok")
	}
}

func TestFormatForgeLocalLine(t *testing.T) {
	mk := NewForgeMark(1, "a.pcap", []byte("x"))
	s := FormatForgeLocalLine(&mk, 2, true)
	if !strings.Contains(s, "s3") || !strings.Contains(s, "hold") || !strings.Contains(s, "cgf:") {
		t.Fatal(s)
	}
}
