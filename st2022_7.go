package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SMPTE ST 2022-7 — seamless protection switching (hitless dual-path).
//
// Production 2022-7 sends *identical* RTP packets on two diverse paths; receivers
// merge by sequence/timestamp. Full hardware packetizers share one SSRC/timeline.
//
// GrokYtalkY implements *sender dual-destination diversity*:
//   - one encode → FFmpeg tee to primary + secondary RTP URLs
//   - matching 2110-20 essence params on both legs
//   - SDP / sidecar announce both destinations
//
// This is the practical software span for live resilience. Bit-identical packet
// cloning across NICs is facility/HW; we signal 2022-7 intent honestly.

const (
	ST2022_7 = "ST 2022-7"
)

// ST20227Config dual-path hitless sender config.
type ST20227Config struct {
	Enabled   bool   `json:"enabled"`
	Primary   string `json:"primary"`   // rtp://…
	Secondary string `json:"secondary"` // rtp://…
	Mode      string `json:"mode"`      // tee | dual-process
	Note      string `json:"note,omitempty"`
}

// ST20227FromURLs builds config when secondary is set.
func ST20227FromURLs(primary, secondary string) ST20227Config {
	secondary = strings.TrimSpace(secondary)
	primary = strings.TrimSpace(primary)
	if secondary == "" || primary == "" {
		return ST20227Config{Enabled: false, Primary: primary}
	}
	return ST20227Config{
		Enabled:   true,
		Primary:   primary,
		Secondary: secondary,
		Mode:      "tee",
		Note: "dual-destination tee (identical essence, two paths). " +
			"Receiver hitless merge assumes facility 2022-7 merge; " +
			"SSRC/timeline identity is best-effort via single ffmpeg process.",
	}
}

// FormatST20227Line one-liner for logs / doctor.
func FormatST20227Line(c ST20227Config) string {
	if !c.Enabled {
		return "ST 2022-7 · off (single destination)"
	}
	return fmt.Sprintf("ST 2022-7 · dual-dest tee · A=%s · B=%s",
		truncate(c.Primary, 40), truncate(c.Secondary, 40))
}

// FormatST20227Basis spec blurb for doctor / docs.
func FormatST20227Basis() string {
	return strings.TrimSpace(`
ST 2022-7 hitless protection (basis)
────────────────────────────────────
• Goal: survive single-path loss without freeze by sending the same RTP
  stream on two diverse network paths; receiver reconstructs hitlessly.
• Requires path diversity (A/B fabric), matching packet timing, and a
  2022-7 capable receiver (merge by seq/timestamp).
• ST 2110 plants often combine 2022-7 with ST 2110-20/30 essences.
• gy venue: --rtp + --rtp-b enables dual-destination tee from one encode.
  Not a full HW packet cloner; production plants may still terminate on
  professional 2022-7 gateways for bit-identical multi-NIC fan-out.
`)
}

// ffmpegTeeRTPArgs builds -f tee dual RTP destinations (single process).
// outSpec is the encoded stream setup already on the command (codec etc. before tee).
// For rawvideo: ... -c:v rawvideo -pix_fmt X -f tee "[f=rtp:…]urlA|[f=rtp:…]urlB"
func ffmpegTeeRTPPayload(primary, secondary string, payloadType int) string {
	// escape not needed for simple rtp URLs without |
	pt := payloadType
	if pt < 1 {
		pt = 96
	}
	// ffmpeg tee: [f=rtp:payload_type=96]rtp://host:port
	a := fmt.Sprintf("[f=rtp:payload_type=%d]%s", pt, primary)
	b := fmt.Sprintf("[f=rtp:payload_type=%d]%s", pt, secondary)
	return a + "|" + b
}

// WriteST20227Sidecar JSON for automation / monitoring.
func WriteST20227Sidecar(path string, c ST20227Config, essence string) error {
	if path == "" {
		return nil
	}
	type side struct {
		Type     string        `json:"type"`
		Standard string        `json:"standard"`
		Config   ST20227Config `json:"config"`
		Essence  string        `json:"essence,omitempty"`
	}
	b, err := json.MarshalIndent(side{
		Type: "st2022-7", Standard: ST2022_7, Config: c, Essence: essence,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// AppendST20227SDPAttrs adds dual-path announcement lines to an SDP body.
func AppendST20227SDPAttrs(body string, c ST20227Config) string {
	if !c.Enabled {
		return body
	}
	extra := fmt.Sprintf(
		"a=x-gy-2022-7:enabled=1;mode=%s\na=x-gy-2022-7-primary:%s\na=x-gy-2022-7-secondary:%s\na=x-gy-2022-7-note:dual-destination-tee-best-effort-hitless\n",
		c.Mode, c.Primary, c.Secondary,
	)
	// insert before first m= line if possible
	if i := strings.Index(body, "\nm="); i >= 0 {
		return body[:i+1] + extra + body[i+1:]
	}
	return body + extra
}
