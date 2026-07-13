package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Cursor-Grok Forge — NFT-style provenance watermark for DOJO multi-pcap streams.
// Marks are carried as KindMeta packets and lightly stamped into hexlum/glyph grids
// so data can be identified as forge-origin without breaking the low-res aesthetic.

const (
	ForgeName    = "Cursor-Grok Forge"
	ForgeIDSpace = "cgf" // short namespace for IDs
)

// ForgeMark is a compact provenance marker (NFT-style identity for a stream/slot).
type ForgeMark struct {
	Type    string `json:"type"`    // "forge-mark"
	Forge   string `json:"forge"`   // Cursor-Grok Forge
	ID      string `json:"id"`      // 16 hex chars (commitment)
	Slot    int    `json:"slot"`    // lab slot 1..N
	Source  string `json:"source"`  // pcap basename or sim
	Content string `json:"content"` // short content hash of first frame / seed
	Version string `json:"v"`
	T       int64  `json:"t"`
}

// NewForgeMark builds a mark from slot + source + optional frame bytes.
func NewForgeMark(slot int, source string, content []byte) ForgeMark {
	if slot < 1 {
		slot = 1
	}
	h := sha256.New()
	_, _ = h.Write([]byte(ForgeName))
	_, _ = h.Write([]byte{byte(slot)})
	_, _ = h.Write([]byte(source))
	_, _ = h.Write(content)
	sum := h.Sum(nil)
	id := hex.EncodeToString(sum[:8])
	ch := hex.EncodeToString(sum[8:12])
	return ForgeMark{
		Type:    "forge-mark",
		Forge:   ForgeName,
		ID:      ForgeIDSpace + ":" + id,
		Slot:    slot,
		Source:  truncate(source, 48),
		Content: ch,
		Version: Version,
		T:       time.Now().UnixMilli(),
	}
}

// Packet returns a KindMeta GYST packet for the mark.
func (m ForgeMark) Packet(seq uint32) StreamPacket {
	b, _ := json.Marshal(m)
	return StreamPacket{
		Kind: KindMeta, Seq: seq, TimeMS: uint64(m.T), Payload: b,
	}
}

// MeshJSON for type:gyst kind=meta or dedicated type.
func (m ForgeMark) MeshJSON(from string) map[string]any {
	return map[string]any{
		"type":    MeshTypeGYST,
		"from":    from,
		"kind":    "meta",
		"w":       0,
		"h":       0,
		"seq":     0,
		"t":       m.T,
		"forge":   m.Forge,
		"mark":    m.ID,
		"slot":    m.Slot,
		"source":  m.Source,
		"content": m.Content,
		"v":       m.Version,
		"b64":     "", // payload optional; fields above enough for bridges
		"meta":    m,
	}
}

// StampHexLum embeds a low-visibility corner marker into hexlum data (in place).
// Uses a 4×4 corner pattern derived from mark ID — NFT-style visual watermark.
// Does not destroy overall image; corner cells encode 16 bits of the id.
func StampHexLum(lum []byte, n int, mark ForgeMark) {
	if n < 8 || len(lum) < n*n {
		return
	}
	// 16-bit fingerprint from id
	bits := markBits(mark.ID)
	// top-left 4×4: encode bits as on/off lattice (values 40 or 200)
	for i := 0; i < 16; i++ {
		x, y := i%4, i/4
		v := byte(40)
		if bits&(1<<uint(i)) != 0 {
			v = 200
		}
		// slight blend so it still looks intentional
		idx := y*n + x
		lum[idx] = v
	}
	// bottom-right 2×2: slot number (1–6) as gray steps
	slot := mark.Slot
	if slot < 1 {
		slot = 1
	}
	if slot > 6 {
		slot = 6
	}
	sv := byte(30 + slot*30)
	lum[(n-1)*n+(n-1)] = sv
	lum[(n-1)*n+(n-2)] = sv
	lum[(n-2)*n+(n-1)] = byte(ForgeIDSpace[0])
	lum[(n-2)*n+(n-2)] = byte(ForgeIDSpace[2]) // 'f'
}

// StampFrame applies hexlum stamp when frame is square-ish small, else stamps via meta only.
func StampFrame(f *FramePixels, mark ForgeMark) {
	if f == nil || f.W < 8 || f.H < 8 {
		return
	}
	// If already small square (glyph-like), stamp RGB as lum
	if f.W == f.H && f.W <= 49 {
		lum := make([]byte, f.W*f.H)
		for i := 0; i < f.W*f.H && i*3+2 < len(f.RGB); i++ {
			r, g, b := int(f.RGB[i*3]), int(f.RGB[i*3+1]), int(f.RGB[i*3+2])
			lum[i] = byte((r*299 + g*587 + b*114) / 1000)
		}
		StampHexLum(lum, f.W, mark)
		for i := 0; i < f.W*f.H; i++ {
			v := lum[i]
			f.RGB[i*3], f.RGB[i*3+1], f.RGB[i*3+2] = v, v, v
		}
		return
	}
	// larger frames: stamp 4×4 corner in RGB
	bits := markBits(mark.ID)
	for i := 0; i < 16 && i < f.W*f.H; i++ {
		x, y := i%4, i/4
		if x >= f.W || y >= f.H {
			continue
		}
		v := byte(40)
		if bits&(1<<uint(i)) != 0 {
			v = 200
		}
		// forge cyan tint for watermark LEDs
		idx := (y*f.W + x) * 3
		f.RGB[idx] = v / 4
		f.RGB[idx+1] = v
		f.RGB[idx+2] = byte(min(255, int(v)+40))
	}
}

// ParseForgeMark from KindMeta payload or mesh map.
func ParseForgeMark(payload []byte) (ForgeMark, bool) {
	var m ForgeMark
	if err := json.Unmarshal(payload, &m); err != nil {
		return m, false
	}
	if m.Type != "forge-mark" && m.Forge == "" {
		return m, false
	}
	if m.Forge == "" {
		m.Forge = ForgeName
	}
	return m, m.ID != ""
}

// ParseForgeFromMesh extracts mark fields from a gyst meta mesh message.
func ParseForgeFromMesh(msg map[string]any) (ForgeMark, bool) {
	if meta, ok := msg["meta"].(map[string]any); ok {
		b, _ := json.Marshal(meta)
		return ParseForgeMark(b)
	}
	id, _ := msg["mark"].(string)
	if id == "" {
		return ForgeMark{}, false
	}
	forge, _ := msg["forge"].(string)
	if forge == "" {
		forge = ForgeName
	}
	slot := 0
	if s, ok := msg["slot"].(float64); ok {
		slot = int(s)
	}
	src, _ := msg["source"].(string)
	content, _ := msg["content"].(string)
	return ForgeMark{
		Type: "forge-mark", Forge: forge, ID: id,
		Slot: slot, Source: src, Content: content,
	}, true
}

func markBits(id string) uint16 {
	h := sha256.Sum256([]byte(id))
	return uint16(h[0])<<8 | uint16(h[1])
}

// FormatMarkLine for TUI sys line.
func FormatMarkLine(m ForgeMark) string {
	return fmt.Sprintf("◈ forge %s · slot %d · %s · %s",
		truncate(m.ID, 20), m.Slot, truncate(m.Source, 16), m.Content)
}

// ShortMarkID is cgf: + first 8 hex of commitment (fit dual Glyph titles).
func ShortMarkID(id string) string {
	if id == "" {
		return ""
	}
	// "cgf:" + 16 hex → show cgf: + 8 hex
	if strings.HasPrefix(id, ForgeIDSpace+":") && len(id) >= 4+8 {
		return id[:4+8]
	}
	return truncate(id, 12)
}

// FormatForgeLocalLine dual-left status: slot index + mark short id.
func FormatForgeLocalLine(mark *ForgeMark, idx0 int, held bool) string {
	hold := ""
	if held {
		hold = " hold"
	}
	if mark == nil {
		return fmt.Sprintf("s%d%s", idx0+1, hold)
	}
	return fmt.Sprintf("s%d %s%s", idx0+1, ShortMarkID(mark.ID), hold)
}

// BurstForgeLocalLabel dual-Glyph left title while multi-slot rotate is active.
func BurstForgeLocalLabel(you string, mark *ForgeMark, idx0 int, rotating bool) string {
	base := you
	if base == "" {
		base = "you"
	}
	if mark == nil || mark.ID == "" {
		if rotating {
			return truncate(fmt.Sprintf("%s s%d", base, idx0+1), 18)
		}
		return truncate(base, 18)
	}
	tag := "s"
	if rotating {
		tag = "↻s"
	}
	return truncate(fmt.Sprintf("%s%d %s", tag, idx0+1, ShortMarkID(mark.ID)), 18)
}

// BurstForgePeerLabel dual-Glyph peer title: nick + short cgf id.
func BurstForgePeerLabel(from string, mark *ForgeMark) string {
	if mark == nil || mark.ID == "" {
		if from != "" {
			return from
		}
		return "peer"
	}
	base := from
	if base == "" {
		base = "forge"
	}
	return truncate(base+" "+ShortMarkID(mark.ID), 18)
}

// BurstForgeStatusLine status under dual Glyph when a forge mark is live RX.
func BurstForgeStatusLine(w int, tx bool, rx, nick string, peers int, mark *ForgeMark) string {
	if mark == nil || mark.ID == "" {
		return BurstStatusLine(w, tx, rx, nick, peers)
	}
	var left string
	switch {
	case tx:
		left = styErr().Reverse(true).Render(" BURST ") + styDim().Render(" · forge TX")
	case rx != "":
		left = styLive().Render("◈ forge") + styDim().Render(fmt.Sprintf(" %s · slot %d · %s",
			ShortMarkID(mark.ID), mark.Slot, truncate(mark.Source, 10)))
	default:
		left = styTitle().Render("◈ forge") + styDim().Render(" "+ShortMarkID(mark.ID)+" · dual Glyph")
	}
	right := styDim().Render(fmt.Sprintf("%d peer", peers))
	if peers != 1 {
		right = styDim().Render(fmt.Sprintf("%d peers", peers))
	}
	need := cellWidth(stripANSI(left)) + cellWidth(stripANSI(right)) + 1
	if need > w {
		return clampCells(left, w)
	}
	gap := w - need
	if gap < 1 {
		gap = 1
	}
	return clampCells(left+strings.Repeat(" ", gap)+right, w)
}
