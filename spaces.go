package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// X Spaces + Periscope/X Media Studio RTMP producer support for GrokYtalkY.
// Burst page models host / 2 co-hosts / 10 speakers / listeners with live
// waveforms; gy space-rtmp pushes A/V to ca.pscp.tv when stream key is ready.

const (
	// X/Periscope Canada ingest (Media Studio Producer · region CA).
	XRTMPURL  = "rtmp://ca.pscp.tv:80/x"
	XRTMPSURL = "rtmps://ca.pscp.tv:443/x"

	// Space role slot budgets (stage layout on burst page + mesh).
	SpaceCohostSlots  = 2
	SpaceSpeakerSlots = 10
)

// SpaceRole is a stage role on an X Space / GrokYtalkY mirror.
type SpaceRole string

const (
	SpaceRoleHost     SpaceRole = "host"
	SpaceRoleCohost   SpaceRole = "cohost"
	SpaceRoleSpeaker  SpaceRole = "speaker"
	SpaceRoleListener SpaceRole = "listener"
)

// SpaceSlot is one named stage seat with optional live level.
type SpaceSlot struct {
	Role   SpaceRole `json:"role"`
	Index  int       `json:"index"` // cohost 0–1 · speaker 0–9
	Nick   string    `json:"nick"`
	Level  float64   `json:"level"` // 0–1 audio level
	Talking bool     `json:"talking"`
	// Placeholder when empty (UI caption)
	Placeholder string `json:"placeholder,omitempty"`
}

// SpaceState is the local producer view of one Space.
type SpaceState struct {
	mu sync.RWMutex

	ID        string // e.g. 1AJEmmANrPeJL
	URL       string // https://x.com/i/spaces/…
	Title     string
	Caption   string // live lower-third / program caption
	Host      SpaceSlot
	Cohosts   [SpaceCohostSlots]SpaceSlot
	Speakers  [SpaceSpeakerSlots]SpaceSlot
	Listeners int
	// Chat ring (newest last)
	Chat []SpaceChatLine
	// RTMP producer
	RTMP SpaceRTMPConfig
	// push process id under media supervisor
	PushID string
}

// SpaceChatLine is one Space chat / caption detail row.
type SpaceChatLine struct {
	From string    `json:"from"`
	Role SpaceRole `json:"role,omitempty"`
	Text string    `json:"text"`
	T    int64     `json:"t"`
	Pin  bool      `json:"pin,omitempty"`
}

// SpaceRTMPConfig holds Media Studio Producer ingest endpoints + key.
type SpaceRTMPConfig struct {
	// Secure prefers RTMPS (default true).
	Secure bool `json:"secure"`
	// BaseURL override (empty → ca.pscp.tv defaults).
	BaseURL string `json:"base_url,omitempty"`
	// StreamKey from X Media Studio · Sources · RTMP (empty = not ready).
	StreamKey string `json:"stream_key,omitempty"`
	// Ready is true when StreamKey is non-empty.
	Ready bool `json:"ready"`
	// Status human line for UI.
	Status string `json:"status"`
}

var (
	spaceOnce sync.Once
	spaceGlob *SpaceState
	// space URL: x.com/i/spaces/ID or twitter.com/i/spaces/ID
	reSpaceURL = regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?(?:x|twitter)\.com/i/spaces/([A-Za-z0-9]+)`)
	reSpaceID  = regexp.MustCompile(`^[A-Za-z0-9]{6,}$`)
)

// Spaces returns the process-wide Space producer state (lazy init).
func Spaces() *SpaceState {
	spaceOnce.Do(func() {
		spaceGlob = NewSpaceState("1AJEmmANrPeJL")
	})
	return spaceGlob
}

// NewSpaceState builds a Space with empty stage placeholders.
func NewSpaceState(id string) *SpaceState {
	id = NormalizeSpaceID(id)
	s := &SpaceState{
		ID:  id,
		URL: SpaceURL(id),
		Title: "GrokYtalkY Space",
		Host: SpaceSlot{
			Role: SpaceRoleHost, Index: 0,
			Placeholder: "Host — you",
		},
		RTMP: SpaceRTMPConfig{
			Secure: true,
			Status: "stream key available when ready",
		},
		Chat: []SpaceChatLine{
			{From: "system", Role: "", Text: "Space stage ready · join roles · paste RTMP key when Media Studio is ready", T: time.Now().UnixMilli()},
			{From: "system", Role: "", Text: "Chat + captions mirror DOJO mesh · /space in TUI · burst.html Spaces panel", T: time.Now().UnixMilli()},
		},
	}
	for i := 0; i < SpaceCohostSlots; i++ {
		s.Cohosts[i] = SpaceSlot{
			Role: SpaceRoleCohost, Index: i,
			Placeholder: fmt.Sprintf("Co-host %d", i+1),
		}
	}
	for i := 0; i < SpaceSpeakerSlots; i++ {
		s.Speakers[i] = SpaceSlot{
			Role: SpaceRoleSpeaker, Index: i,
			Placeholder: fmt.Sprintf("Speaker %d", i+1),
		}
	}
	// env stream key if operator already has one
	if k := strings.TrimSpace(os.Getenv("GY_X_STREAM_KEY")); k != "" {
		s.RTMP.StreamKey = k
		s.RTMP.Ready = true
		s.RTMP.Status = "stream key set (GY_X_STREAM_KEY)"
	}
	if u := strings.TrimSpace(os.Getenv("GY_X_RTMP_URL")); u != "" {
		s.RTMP.BaseURL = u
		s.RTMP.Secure = strings.HasPrefix(strings.ToLower(u), "rtmps")
	}
	return s
}

// NormalizeSpaceID extracts id from URL or bare id.
func NormalizeSpaceID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "1AJEmmANrPeJL"
	}
	if m := reSpaceURL.FindStringSubmatch(raw); len(m) == 2 {
		return m[1]
	}
	// strip query
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.Trim(raw, "/")
	if reSpaceID.MatchString(raw) {
		return raw
	}
	return raw
}

// ParseSpaceURL returns space id and canonical https URL.
func ParseSpaceURL(raw string) (id, canon string, ok bool) {
	id = NormalizeSpaceID(raw)
	if id == "" || !reSpaceID.MatchString(id) {
		// still allow non-standard ids if extracted from x.com path
		if m := reSpaceURL.FindStringSubmatch(raw); len(m) == 2 {
			id = m[1]
		} else if !reSpaceID.MatchString(id) {
			return "", "", false
		}
	}
	return id, SpaceURL(id), true
}

// SpaceURL is the public X Spaces link.
func SpaceURL(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "https://x.com/i/spaces"
	}
	return "https://x.com/i/spaces/" + id
}

// BaseRTMPURL returns the configured ingest base (no stream key).
func (c SpaceRTMPConfig) BaseRTMPURL() string {
	if b := strings.TrimSpace(c.BaseURL); b != "" {
		return strings.TrimRight(b, "/")
	}
	if c.Secure {
		return XRTMPSURL
	}
	return XRTMPURL
}

// PublishURL appends stream key for ffmpeg -f flv target.
// Empty key → base only (not ready to publish).
func (c SpaceRTMPConfig) PublishURL() (string, error) {
	base := c.BaseRTMPURL()
	key := strings.TrimSpace(c.StreamKey)
	if key == "" {
		return "", fmt.Errorf("stream key not ready — paste from X Media Studio Sources (RTMP)")
	}
	// Periscope/X: rtmps://host:443/x/<stream_key>
	return base + "/" + key, nil
}

// SetStreamKey stores key and updates Ready/Status.
func (s *SpaceState) SetStreamKey(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key = strings.TrimSpace(key)
	s.RTMP.StreamKey = key
	s.RTMP.Ready = key != ""
	if key == "" {
		s.RTMP.Status = "stream key available when ready"
	} else {
		s.RTMP.Status = "stream key ready · RTMP publish armed"
	}
}

// SetSecure toggles RTMPS vs RTMP defaults.
func (s *SpaceState) SetSecure(secure bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RTMP.Secure = secure
	if s.RTMP.BaseURL == "" || strings.Contains(s.RTMP.BaseURL, "pscp.tv") {
		// reset to matching default when on pscp defaults
		s.RTMP.BaseURL = ""
	}
}

// Snapshot returns a copy for UI/JSON (shallow chat).
func (s *SpaceState) Snapshot() SpaceState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := SpaceState{
		ID: s.ID, URL: s.URL, Title: s.Title, Caption: s.Caption,
		Host: s.Host, Cohosts: s.Cohosts, Speakers: s.Speakers,
		Listeners: s.Listeners, RTMP: s.RTMP, PushID: s.PushID,
	}
	if len(s.Chat) > 0 {
		out.Chat = append([]SpaceChatLine(nil), s.Chat...)
	}
	return out
}

// SetID updates space id + URL.
func (s *SpaceState) SetID(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := NormalizeSpaceID(raw)
	s.ID = id
	s.URL = SpaceURL(id)
}

// SetCaption sets lower-third / Space caption text.
func (s *SpaceState) SetCaption(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Caption = strings.TrimSpace(text)
}

// PushChat appends a chat line (ring max 200).
func (s *SpaceState) PushChat(line SpaceChatLine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if line.T == 0 {
		line.T = time.Now().UnixMilli()
	}
	s.Chat = append(s.Chat, line)
	if len(s.Chat) > 200 {
		s.Chat = s.Chat[len(s.Chat)-200:]
	}
}

// Seat fills a stage seat by role + index (cohost 0–1, speaker 0–9).
func (s *SpaceState) Seat(role SpaceRole, index int, nick string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	nick = strings.TrimSpace(nick)
	switch role {
	case SpaceRoleHost:
		s.Host.Nick = nick
		s.Host.Talking = nick != ""
	case SpaceRoleCohost:
		if index < 0 || index >= SpaceCohostSlots {
			return fmt.Errorf("cohost index 0–%d", SpaceCohostSlots-1)
		}
		s.Cohosts[index].Nick = nick
		s.Cohosts[index].Talking = false
	case SpaceRoleSpeaker:
		if index < 0 || index >= SpaceSpeakerSlots {
			return fmt.Errorf("speaker index 0–%d", SpaceSpeakerSlots-1)
		}
		s.Speakers[index].Nick = nick
		s.Speakers[index].Talking = false
	default:
		return fmt.Errorf("role %q not seatable (use host|cohost|speaker)", role)
	}
	return nil
}

// SetLevel updates audio level for a seat (waveform).
func (s *SpaceState) SetLevel(role SpaceRole, index int, level float64) {
	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	talking := level > 0.08
	switch role {
	case SpaceRoleHost:
		s.Host.Level = level
		s.Host.Talking = talking
	case SpaceRoleCohost:
		if index >= 0 && index < SpaceCohostSlots {
			s.Cohosts[index].Level = level
			s.Cohosts[index].Talking = talking
		}
	case SpaceRoleSpeaker:
		if index >= 0 && index < SpaceSpeakerSlots {
			s.Speakers[index].Level = level
			s.Speakers[index].Talking = talking
		}
	}
}

// SetListeners sets listener count.
func (s *SpaceState) SetListeners(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 0 {
		n = 0
	}
	s.Listeners = n
}

// MeshRosterJSON for hub fan-out type:space-roster.
func (s *SpaceState) MeshRosterJSON(from string) map[string]any {
	snap := s.Snapshot()
	co := make([]map[string]any, SpaceCohostSlots)
	for i, c := range snap.Cohosts {
		co[i] = map[string]any{"index": i, "nick": c.Nick, "level": c.Level, "talking": c.Talking}
	}
	sp := make([]map[string]any, SpaceSpeakerSlots)
	for i, c := range snap.Speakers {
		sp[i] = map[string]any{"index": i, "nick": c.Nick, "level": c.Level, "talking": c.Talking}
	}
	return map[string]any{
		"type":      "space-roster",
		"from":      from,
		"space":     snap.ID,
		"url":       snap.URL,
		"title":     snap.Title,
		"caption":   snap.Caption,
		"host":      map[string]any{"nick": snap.Host.Nick, "level": snap.Host.Level, "talking": snap.Host.Talking},
		"cohosts":   co,
		"speakers":  sp,
		"listeners": snap.Listeners,
		"t":         time.Now().UnixMilli(),
	}
}

// FormatSpaceDoctor multi-line for gy doctor space / /space status.
func FormatSpaceDoctor(s *SpaceState) string {
	if s == nil {
		s = Spaces()
	}
	snap := s.Snapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "space · %s\n", snap.ID)
	fmt.Fprintf(&b, "  url       %s\n", snap.URL)
	fmt.Fprintf(&b, "  title     %s\n", emptyDash(snap.Title))
	fmt.Fprintf(&b, "  caption   %s\n", emptyDash(snap.Caption))
	fmt.Fprintf(&b, "  host      %s  lv=%.2f\n", emptyDash(snap.Host.Nick), snap.Host.Level)
	for i, c := range snap.Cohosts {
		fmt.Fprintf(&b, "  cohost[%d] %s  lv=%.2f\n", i, emptyDash(c.Nick), c.Level)
	}
	activeSp := 0
	for _, c := range snap.Speakers {
		if c.Nick != "" {
			activeSp++
		}
	}
	fmt.Fprintf(&b, "  speakers  %d/%d seated\n", activeSp, SpaceSpeakerSlots)
	fmt.Fprintf(&b, "  listeners %d\n", snap.Listeners)
	fmt.Fprintf(&b, "  rtmp      %s\n", snap.RTMP.BaseRTMPURL())
	fmt.Fprintf(&b, "  key       %s\n", rtmpKeyStatus(snap.RTMP))
	fmt.Fprintf(&b, "  status    %s\n", snap.RTMP.Status)
	if snap.PushID != "" {
		fmt.Fprintf(&b, "  push      id=%s\n", snap.PushID)
	}
	fmt.Fprintf(&b, "  slots     host + %d cohosts + %d speakers + listeners\n", SpaceCohostSlots, SpaceSpeakerSlots)
	fmt.Fprintf(&b, "  burst     site/burst.html · Spaces stage + waveforms\n")
	fmt.Fprintf(&b, "  push cli  gy space-rtmp --key $KEY [--in cam|file|url]\n")
	return b.String()
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func rtmpKeyStatus(c SpaceRTMPConfig) string {
	if !c.Ready || c.StreamKey == "" {
		return "(available when ready)"
	}
	// mask key
	k := c.StreamKey
	if len(k) > 8 {
		return k[:4] + "…" + k[len(k)-4:]
	}
	return "****"
}

// BuildSpaceRTMPArgs constructs ffmpeg args for X/Periscope FLV publish.
// input is ffmpeg -i source (file, URL, or avfoundation/x11grab style).
func BuildSpaceRTMPArgs(input string, cfg SpaceRTMPConfig) ([]string, string, error) {
	pub, err := cfg.PublishURL()
	if err != nil {
		return nil, "", err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, "", fmt.Errorf("input required (file, URL, or device)")
	}
	// video+audio → H.264/AAC FLV (X Media Studio friendly)
	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-re",
		"-i", input,
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
		"-pix_fmt", "yuv420p", "-g", "50",
		"-b:v", "2500k", "-maxrate", "2500k", "-bufsize", "5000k",
		"-c:a", "aac", "-b:a", "128k", "-ar", "44100", "-ac", "2",
		"-f", "flv",
		pub,
	}
	return args, pub, nil
}

// StartSpaceRTMPPush launches supervised ffmpeg publish. Returns media id.
func StartSpaceRTMPPush(input string, cfg SpaceRTMPConfig) (string, error) {
	args, pub, err := BuildSpaceRTMPArgs(input, cfg)
	if err != nil {
		return "", err
	}
	// resolve ffmpeg binary
	bin := "ffmpeg"
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		bin = p
	}
	label := "space-rtmp " + shortRTMPURL(pub)
	_, id, err := StartRegistered(MediaKindPub, label, bin, args)
	if err != nil {
		return "", err
	}
	return id, nil
}

func shortRTMPURL(u string) string {
	// hide stream key tail for logs
	if i := strings.LastIndex(u, "/"); i > 0 && i < len(u)-1 {
		return u[:i+1] + "…"
	}
	return u
}

// runSpaceRTMPCmd: gy space-rtmp --key KEY --in SRC [--rtmp|--rtmps]
func runSpaceRTMPCmd(args []string) error {
	fs := newBridgeFlagSet("space-rtmp")
	key := fs.String("key", "", "X Media Studio RTMP stream key (or GY_X_STREAM_KEY)")
	in := fs.String("in", "", "ffmpeg input (file, URL, avfoundation:…)")
	secure := fs.Bool("rtmps", true, "use rtmps://ca.pscp.tv:443/x (default)")
	plain := fs.Bool("rtmp", false, "use rtmp://ca.pscp.tv:80/x")
	base := fs.String("url", "", "override base URL (default pscp.tv CA)")
	spaceID := fs.String("space", "", "Space id or URL (metadata only)")
	dry := fs.Bool("dry-run", false, "print ffmpeg target without starting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	k := strings.TrimSpace(*key)
	if k == "" {
		k = strings.TrimSpace(os.Getenv("GY_X_STREAM_KEY"))
	}
	cfg := SpaceRTMPConfig{
		Secure:    !*plain && *secure,
		BaseURL:   strings.TrimSpace(*base),
		StreamKey: k,
		Ready:     k != "",
	}
	if *plain {
		cfg.Secure = false
	}
	if *spaceID != "" {
		Spaces().SetID(*spaceID)
	}
	Spaces().SetStreamKey(k)
	Spaces().SetSecure(cfg.Secure)

	if *dry {
		pub, err := cfg.PublishURL()
		if err != nil {
			fmt.Println("space-rtmp · not ready:", err)
			fmt.Println("  base:", cfg.BaseRTMPURL())
			fmt.Println("  key:  (available when ready)")
			return err
		}
		argsFF, _, _ := BuildSpaceRTMPArgs(nz(*in, "INPUT"), cfg)
		fmt.Println("space-rtmp · dry-run")
		fmt.Println("  publish:", shortRTMPURL(pub))
		fmt.Println("  ffmpeg:", "ffmpeg", strings.Join(argsFF, " "))
		return nil
	}
	if strings.TrimSpace(*in) == "" {
		return fmt.Errorf("space-rtmp: --in required (or --dry-run)\n  gy space-rtmp --key $KEY --in video.mp4\n  gy space-rtmp --key $KEY --in \"avfoundation:0:0\"  # mac cam+mic")
	}
	id, err := StartSpaceRTMPPush(*in, cfg)
	if err != nil {
		return err
	}
	Spaces().mu.Lock()
	Spaces().PushID = id
	Spaces().mu.Unlock()
	fmt.Printf("space-rtmp · pushing id=%s → %s\n", id, cfg.BaseRTMPURL()+"/…")
	fmt.Println("  stop: gy media kill  ·  or Media().Kill(id)")
	// block until interrupt if attached
	fmt.Println("  ffmpeg supervised under media registry · process stays up with gy")
	return nil
}

func nz(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// runSpaceCmd: gy space [status|id|key|caption|…]
func runSpaceCmd(args []string) error {
	if len(args) == 0 {
		fmt.Print(FormatSpaceDoctor(Spaces()))
		return nil
	}
	sub := strings.ToLower(args[0])
	rest := ""
	if len(args) > 1 {
		rest = strings.Join(args[1:], " ")
	}
	switch sub {
	case "status", "doctor", "show":
		fmt.Print(FormatSpaceDoctor(Spaces()))
	case "id", "url", "open":
		if rest == "" {
			fmt.Println(Spaces().Snapshot().URL)
			return nil
		}
		Spaces().SetID(rest)
		fmt.Print(FormatSpaceDoctor(Spaces()))
	case "key", "stream-key":
		if rest == "" {
			fmt.Println(rtmpKeyStatus(Spaces().Snapshot().RTMP))
			return nil
		}
		Spaces().SetStreamKey(rest)
		fmt.Println("stream key set ·", rtmpKeyStatus(Spaces().Snapshot().RTMP))
	case "rtmp":
		Spaces().SetSecure(false)
		fmt.Println("ingest →", XRTMPURL)
	case "rtmps":
		Spaces().SetSecure(true)
		fmt.Println("ingest →", XRTMPSURL)
	case "caption", "cap":
		Spaces().SetCaption(rest)
		fmt.Println("caption →", emptyDash(rest))
	case "listeners", "n":
		n, _ := strconv.Atoi(strings.TrimSpace(rest))
		Spaces().SetListeners(n)
		fmt.Println("listeners →", n)
	case "seat":
		// gy space seat host|cohost:0|speaker:3 nick
		parts := strings.Fields(rest)
		if len(parts) < 2 {
			return fmt.Errorf("usage: gy space seat host|cohost:N|speaker:N <nick>")
		}
		role, idx, err := parseSeatSpec(parts[0])
		if err != nil {
			return err
		}
		nick := strings.Join(parts[1:], " ")
		if err := Spaces().Seat(role, idx, nick); err != nil {
			return err
		}
		fmt.Printf("seated %s[%d] → %s\n", role, idx, nick)
	case "help", "-h":
		fmt.Print(spaceCmdHelp())
	default:
		// treat as space id/url
		Spaces().SetID(sub)
		if rest != "" {
			Spaces().SetID(sub + " " + rest)
		}
		fmt.Print(FormatSpaceDoctor(Spaces()))
	}
	return nil
}

func parseSeatSpec(spec string) (SpaceRole, int, error) {
	spec = strings.ToLower(strings.TrimSpace(spec))
	if spec == "host" {
		return SpaceRoleHost, 0, nil
	}
	if strings.HasPrefix(spec, "cohost") {
		idx := 0
		if i := strings.IndexByte(spec, ':'); i >= 0 {
			idx, _ = strconv.Atoi(spec[i+1:])
		} else if i := strings.IndexByte(spec, '-'); i >= 0 {
			idx, _ = strconv.Atoi(spec[i+1:])
		}
		return SpaceRoleCohost, idx, nil
	}
	if strings.HasPrefix(spec, "speaker") || strings.HasPrefix(spec, "spk") {
		idx := 0
		if i := strings.IndexByte(spec, ':'); i >= 0 {
			idx, _ = strconv.Atoi(spec[i+1:])
		} else if i := strings.IndexByte(spec, '-'); i >= 0 {
			idx, _ = strconv.Atoi(spec[i+1:])
		}
		return SpaceRoleSpeaker, idx, nil
	}
	return "", 0, fmt.Errorf("seat spec host|cohost:N|speaker:N")
}

func spaceCmdHelp() string {
	return `gy space — X Spaces stage + RTMP producer

  gy space                         status (id, roster, rtmp)
  gy space id <url|id>             bind Space (default 1AJEmmANrPeJL)
  gy space key <stream_key>        Media Studio RTMP key
  gy space rtmp | rtmps            select ca.pscp.tv ingest
  gy space caption <text>          lower-third / Space caption
  gy space seat host <nick>
  gy space seat cohost:0 <nick>
  gy space seat speaker:3 <nick>
  gy space listeners 42

  gy space-rtmp --key KEY --in SRC [--rtmps|--rtmp] [--dry-run]
    RTMPS  rtmps://ca.pscp.tv:443/x/<key>
    RTMP   rtmp://ca.pscp.tv:80/x/<key>
    env    GY_X_STREAM_KEY · GY_X_RTMP_URL

  burst page: site/burst.html  (waveforms · chat · captions · RTMP panel)
  example:    https://x.com/i/spaces/1AJEmmANrPeJL
`
}

// ApplySpaceMeshInbound updates local state from mesh space-* messages.
func ApplySpaceMeshInbound(msg map[string]any) {
	if msg == nil {
		return
	}
	typ, _ := msg["type"].(string)
	s := Spaces()
	switch typ {
	case "space-roster":
		if id, _ := msg["space"].(string); id != "" {
			s.SetID(id)
		}
		if cap, _ := msg["caption"].(string); cap != "" {
			s.SetCaption(cap)
		}
		if n, ok := msg["listeners"].(float64); ok {
			s.SetListeners(int(n))
		}
		if h, ok := msg["host"].(map[string]any); ok {
			if n, _ := h["nick"].(string); n != "" {
				_ = s.Seat(SpaceRoleHost, 0, n)
			}
			if lv, ok := h["level"].(float64); ok {
				s.SetLevel(SpaceRoleHost, 0, lv)
			}
		}
		if arr, ok := msg["cohosts"].([]any); ok {
			for _, it := range arr {
				m, _ := it.(map[string]any)
				if m == nil {
					continue
				}
				idx := int(asFloat(m["index"]))
				if n, _ := m["nick"].(string); n != "" {
					_ = s.Seat(SpaceRoleCohost, idx, n)
				}
				if lv, ok := m["level"].(float64); ok {
					s.SetLevel(SpaceRoleCohost, idx, lv)
				}
			}
		}
		if arr, ok := msg["speakers"].([]any); ok {
			for _, it := range arr {
				m, _ := it.(map[string]any)
				if m == nil {
					continue
				}
				idx := int(asFloat(m["index"]))
				if n, _ := m["nick"].(string); n != "" {
					_ = s.Seat(SpaceRoleSpeaker, idx, n)
				}
				if lv, ok := m["level"].(float64); ok {
					s.SetLevel(SpaceRoleSpeaker, idx, lv)
				}
			}
		}
	case "space-level":
		role := SpaceRole(strings.ToLower(fmt.Sprint(msg["role"])))
		idx := int(asFloat(msg["slot"]))
		lv := asFloat(msg["level"])
		s.SetLevel(role, idx, lv)
	case "space-chat", "space-caption":
		from, _ := msg["from"].(string)
		text, _ := msg["text"].(string)
		if text == "" {
			text, _ = msg["caption"].(string)
		}
		role := SpaceRole(strings.ToLower(fmt.Sprint(msg["role"])))
		if text != "" {
			s.PushChat(SpaceChatLine{From: from, Role: role, Text: text, T: int64(asFloat(msg["t"]))})
		}
		if typ == "space-caption" && text != "" {
			s.SetCaption(text)
		}
	}
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	default:
		return 0
	}
}

// ValidateRTMPBase ensures URL looks like rtmp(s) ingest.
func ValidateRTMPBase(u string) error {
	u = strings.TrimSpace(u)
	if u == "" {
		return fmt.Errorf("empty rtmp url")
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return err
	}
	sch := strings.ToLower(parsed.Scheme)
	if sch != "rtmp" && sch != "rtmps" {
		return fmt.Errorf("scheme must be rtmp or rtmps, got %s", parsed.Scheme)
	}
	return nil
}
