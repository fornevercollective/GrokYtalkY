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
	Role    SpaceRole `json:"role"`
	Index   int       `json:"index"` // cohost 0–1 · speaker 0–9
	Nick    string    `json:"nick"`
	Level   float64   `json:"level"` // 0–1 audio level
	Talking bool      `json:"talking"`
	// Muted by host (or self). Waveforms freeze; mesh levels ignored while true.
	Muted   bool   `json:"muted"`
	MutedBy string `json:"muted_by,omitempty"`
	// Placeholder when empty (UI caption)
	Placeholder string `json:"placeholder,omitempty"`
}

// SpaceListener is one named listener (not on stage).
type SpaceListener struct {
	Nick     string `json:"nick"`
	ID       string `json:"id,omitempty"`
	JoinedAt int64  `json:"joined_at,omitempty"`
}

// SpaceAsset advertises this gy process as a reusable X.com stream asset
// (other users can seat/request publish through this operator).
type SpaceAsset struct {
	// Offer true when operator is offering gy as stream asset.
	Offer bool `json:"offer"`
	// Operator nick (host / stream owner).
	Operator string `json:"operator,omitempty"`
	// Label human name for the asset (e.g. "fornevercollective gy").
	Label string `json:"label,omitempty"`
	// Guests allowed to request seats / publish via this asset.
	Guests []string `json:"guests,omitempty"`
	// Public true → announce on mesh for anyone in room.
	Public bool `json:"public"`
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
	// Listeners is len(ListenerList); kept for back-compat mesh field.
	Listeners    int
	ListenerList []SpaceListener
	// MuteAll when host mutes entire stage (except host).
	MuteAll   bool
	MuteAllBy string
	// Chat ring (newest last)
	Chat []SpaceChatLine
	// RTMP producer
	RTMP SpaceRTMPConfig
	// Asset: gy as X streaming asset for other users
	Asset SpaceAsset
	// push process id under media supervisor
	PushID string
	// KeySource last successful auto-pull origin (env|file|json|clipboard|url)
	KeySource string
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
	// KeyPath optional file to auto-pull (also GY_X_STREAM_KEY_FILE).
	KeyPath string `json:"key_path,omitempty"`
	// KeyURL optional HTTPS endpoint that returns plain key or JSON {stream_key}.
	KeyURL string `json:"key_url,omitempty"`
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
	s.Asset = SpaceAsset{
		Label:  "GrokYtalkY stream asset",
		Public: false,
	}
	if p := strings.TrimSpace(os.Getenv("GY_X_STREAM_KEY_FILE")); p != "" {
		s.RTMP.KeyPath = p
	}
	if u := strings.TrimSpace(os.Getenv("GY_X_STREAM_KEY_URL")); u != "" {
		s.RTMP.KeyURL = u
	}
	if u := strings.TrimSpace(os.Getenv("GY_X_RTMP_URL")); u != "" {
		s.RTMP.BaseURL = u
		s.RTMP.Secure = strings.HasPrefix(strings.ToLower(u), "rtmps")
	}
	// auto-pull on init (env/file/json) — never fails startup
	if src, key, err := PullStreamKey(PullKeyOpts{}); err == nil && key != "" {
		s.RTMP.StreamKey = key
		s.RTMP.Ready = true
		s.RTMP.Status = "stream key auto-pulled · " + src
		s.KeySource = src
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
	s.SetStreamKeyFrom(key, "manual")
}

// SetStreamKeyFrom records origin (env|file|json|clipboard|url|manual).
func (s *SpaceState) SetStreamKeyFrom(key, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key = strings.TrimSpace(key)
	s.RTMP.StreamKey = key
	s.RTMP.Ready = key != ""
	s.KeySource = source
	if key == "" {
		s.RTMP.Status = "stream key available when ready"
	} else if source != "" && source != "manual" {
		s.RTMP.Status = "stream key ready · auto-pulled from " + source
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

// Snapshot returns a copy for UI/JSON (shallow chat + listeners).
func (s *SpaceState) Snapshot() SpaceState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := SpaceState{
		ID: s.ID, URL: s.URL, Title: s.Title, Caption: s.Caption,
		Host: s.Host, Cohosts: s.Cohosts, Speakers: s.Speakers,
		Listeners: s.Listeners, MuteAll: s.MuteAll, MuteAllBy: s.MuteAllBy,
		RTMP: s.RTMP, Asset: s.Asset, PushID: s.PushID, KeySource: s.KeySource,
	}
	if len(s.Chat) > 0 {
		out.Chat = append([]SpaceChatLine(nil), s.Chat...)
	}
	if len(s.ListenerList) > 0 {
		out.ListenerList = append([]SpaceListener(nil), s.ListenerList...)
	}
	if len(s.Asset.Guests) > 0 {
		out.Asset.Guests = append([]string(nil), s.Asset.Guests...)
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

// SetLevel updates audio level for a seat (waveform). Muted seats stay at 0.
func (s *SpaceState) SetLevel(role SpaceRole, index int, level float64) {
	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// stage mute-all silences non-host
	if s.MuteAll && role != SpaceRoleHost {
		level = 0
	}
	talking := level > 0.08
	switch role {
	case SpaceRoleHost:
		if s.Host.Muted {
			level, talking = 0, false
		}
		s.Host.Level = level
		s.Host.Talking = talking
	case SpaceRoleCohost:
		if index >= 0 && index < SpaceCohostSlots {
			if s.Cohosts[index].Muted {
				level, talking = 0, false
			}
			s.Cohosts[index].Level = level
			s.Cohosts[index].Talking = talking
		}
	case SpaceRoleSpeaker:
		if index >= 0 && index < SpaceSpeakerSlots {
			if s.Speakers[index].Muted {
				level, talking = 0, false
			}
			s.Speakers[index].Level = level
			s.Speakers[index].Talking = talking
		}
	}
}

// SetMute sets host (or self) mute on a seat.
func (s *SpaceState) SetMute(role SpaceRole, index int, muted bool, by string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	by = strings.TrimSpace(by)
	apply := func(slot *SpaceSlot) {
		slot.Muted = muted
		if muted {
			slot.MutedBy = by
			slot.Level = 0
			slot.Talking = false
		} else {
			slot.MutedBy = ""
		}
	}
	switch role {
	case SpaceRoleHost:
		apply(&s.Host)
	case SpaceRoleCohost:
		if index < 0 || index >= SpaceCohostSlots {
			return fmt.Errorf("cohost index 0–%d", SpaceCohostSlots-1)
		}
		apply(&s.Cohosts[index])
	case SpaceRoleSpeaker:
		if index < 0 || index >= SpaceSpeakerSlots {
			return fmt.Errorf("speaker index 0–%d", SpaceSpeakerSlots-1)
		}
		apply(&s.Speakers[index])
	default:
		return fmt.Errorf("cannot mute role %q", role)
	}
	return nil
}

// SetMuteAll host control: mute entire stage except host.
func (s *SpaceState) SetMuteAll(on bool, by string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MuteAll = on
	s.MuteAllBy = strings.TrimSpace(by)
	if on {
		for i := range s.Cohosts {
			s.Cohosts[i].Muted = true
			s.Cohosts[i].MutedBy = s.MuteAllBy
			s.Cohosts[i].Level = 0
			s.Cohosts[i].Talking = false
		}
		for i := range s.Speakers {
			s.Speakers[i].Muted = true
			s.Speakers[i].MutedBy = s.MuteAllBy
			s.Speakers[i].Level = 0
			s.Speakers[i].Talking = false
		}
	} else {
		for i := range s.Cohosts {
			s.Cohosts[i].Muted = false
			s.Cohosts[i].MutedBy = ""
		}
		for i := range s.Speakers {
			s.Speakers[i].Muted = false
			s.Speakers[i].MutedBy = ""
		}
		s.MuteAllBy = ""
	}
}

// IsMuted reports seat mute (including mute-all for non-host).
func (s *SpaceState) IsMuted(role SpaceRole, index int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.MuteAll && role != SpaceRoleHost {
		return true
	}
	switch role {
	case SpaceRoleHost:
		return s.Host.Muted
	case SpaceRoleCohost:
		if index >= 0 && index < SpaceCohostSlots {
			return s.Cohosts[index].Muted
		}
	case SpaceRoleSpeaker:
		if index >= 0 && index < SpaceSpeakerSlots {
			return s.Speakers[index].Muted
		}
	}
	return false
}

// AddListener registers a listener by nick (idempotent).
func (s *SpaceState) AddListener(nick, id string) {
	nick = strings.TrimSpace(nick)
	if nick == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, l := range s.ListenerList {
		if strings.EqualFold(l.Nick, nick) {
			if id != "" {
				s.ListenerList[i].ID = id
			}
			s.Listeners = len(s.ListenerList)
			return
		}
	}
	s.ListenerList = append(s.ListenerList, SpaceListener{
		Nick: nick, ID: id, JoinedAt: time.Now().UnixMilli(),
	})
	// soft cap
	if len(s.ListenerList) > 500 {
		s.ListenerList = s.ListenerList[len(s.ListenerList)-500:]
	}
	s.Listeners = len(s.ListenerList)
}

// RemoveListener drops a listener by nick.
func (s *SpaceState) RemoveListener(nick string) {
	nick = strings.TrimSpace(nick)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.ListenerList[:0]
	for _, l := range s.ListenerList {
		if !strings.EqualFold(l.Nick, nick) {
			out = append(out, l)
		}
	}
	s.ListenerList = out
	s.Listeners = len(s.ListenerList)
}

// SetListeners sets listener count (pads/truncates anonymous placeholders).
func (s *SpaceState) SetListeners(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 0 {
		n = 0
	}
	// keep named listeners; pad with anon if n larger
	if n <= len(s.ListenerList) {
		s.ListenerList = s.ListenerList[:n]
	} else {
		for len(s.ListenerList) < n {
			s.ListenerList = append(s.ListenerList, SpaceListener{
				Nick: fmt.Sprintf("listener-%d", len(s.ListenerList)+1),
				JoinedAt: time.Now().UnixMilli(),
			})
		}
	}
	s.Listeners = len(s.ListenerList)
}

// SetAssetOffer enables/disables gy as X stream asset for other users.
func (s *SpaceState) SetAssetOffer(on bool, operator, label string, public bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Asset.Offer = on
	if operator != "" {
		s.Asset.Operator = operator
	}
	if label != "" {
		s.Asset.Label = label
	}
	s.Asset.Public = public
}

// AllowGuest adds a nick allowed to use this stream asset.
func (s *SpaceState) AllowGuest(nick string) {
	nick = strings.TrimSpace(nick)
	if nick == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.Asset.Guests {
		if strings.EqualFold(g, nick) {
			return
		}
	}
	s.Asset.Guests = append(s.Asset.Guests, nick)
}

// MeshMuteJSON type:space-mute
func (s *SpaceState) MeshMuteJSON(from string, role SpaceRole, index int, muted bool) map[string]any {
	return map[string]any{
		"type": "space-mute", "from": from, "role": string(role),
		"slot": index, "muted": muted, "by": from,
		"space": s.Snapshot().ID, "t": time.Now().UnixMilli(),
	}
}

// MeshMuteAllJSON type:space-mute-all
func (s *SpaceState) MeshMuteAllJSON(from string, muted bool) map[string]any {
	return map[string]any{
		"type": "space-mute-all", "from": from, "muted": muted, "by": from,
		"space": s.Snapshot().ID, "t": time.Now().UnixMilli(),
	}
}

// MeshAssetJSON type:space-asset — advertise gy as stream asset.
func (s *SpaceState) MeshAssetJSON(from string) map[string]any {
	snap := s.Snapshot()
	return map[string]any{
		"type":     "space-asset",
		"from":     from,
		"offer":    snap.Asset.Offer,
		"operator": nz(snap.Asset.Operator, from),
		"label":    snap.Asset.Label,
		"public":   snap.Asset.Public,
		"guests":   snap.Asset.Guests,
		"space":    snap.ID,
		"url":      snap.URL,
		"rtmp":     snap.RTMP.BaseRTMPURL(),
		"ready":    snap.RTMP.Ready,
		"key":      snap.RTMP.Ready, // bool only — never leak key on mesh
		"t":        time.Now().UnixMilli(),
	}
}

// PublicAPISnapshot for GET /api/space (never includes stream key).
func (s *SpaceState) PublicAPISnapshot() map[string]any {
	snap := s.Snapshot()
	co := make([]map[string]any, SpaceCohostSlots)
	for i, c := range snap.Cohosts {
		co[i] = map[string]any{"index": i, "nick": c.Nick, "level": c.Level, "talking": c.Talking, "muted": c.Muted}
	}
	sp := make([]map[string]any, SpaceSpeakerSlots)
	for i, c := range snap.Speakers {
		sp[i] = map[string]any{"index": i, "nick": c.Nick, "level": c.Level, "talking": c.Talking, "muted": c.Muted}
	}
	listeners := make([]map[string]any, 0, len(snap.ListenerList))
	for _, l := range snap.ListenerList {
		listeners = append(listeners, map[string]any{"nick": l.Nick, "joined_at": l.JoinedAt})
	}
	return map[string]any{
		"space": snap.ID, "url": snap.URL, "title": snap.Title, "caption": snap.Caption,
		"host": map[string]any{"nick": snap.Host.Nick, "level": snap.Host.Level, "talking": snap.Host.Talking, "muted": snap.Host.Muted},
		"cohosts": co, "speakers": sp,
		"listeners": len(listeners), "listener_list": listeners,
		"mute_all": snap.MuteAll,
		"rtmp": map[string]any{
			"base": snap.RTMP.BaseRTMPURL(), "secure": snap.RTMP.Secure,
			"ready": snap.RTMP.Ready, "status": snap.RTMP.Status,
			"key_masked": rtmpKeyStatus(snap.RTMP), "key_source": snap.KeySource,
		},
		"asset": snap.Asset,
		"push":  snap.PushID != "",
		"version": Version,
	}
}

// MeshRosterJSON for hub fan-out type:space-roster.
func (s *SpaceState) MeshRosterJSON(from string) map[string]any {
	snap := s.Snapshot()
	co := make([]map[string]any, SpaceCohostSlots)
	for i, c := range snap.Cohosts {
		co[i] = map[string]any{"index": i, "nick": c.Nick, "level": c.Level, "talking": c.Talking, "muted": c.Muted}
	}
	sp := make([]map[string]any, SpaceSpeakerSlots)
	for i, c := range snap.Speakers {
		sp[i] = map[string]any{"index": i, "nick": c.Nick, "level": c.Level, "talking": c.Talking, "muted": c.Muted}
	}
	ll := make([]map[string]any, 0, len(snap.ListenerList))
	for _, l := range snap.ListenerList {
		ll = append(ll, map[string]any{"nick": l.Nick, "joined_at": l.JoinedAt})
	}
	return map[string]any{
		"type":      "space-roster",
		"from":      from,
		"space":     snap.ID,
		"url":       snap.URL,
		"title":     snap.Title,
		"caption":   snap.Caption,
		"host":      map[string]any{"nick": snap.Host.Nick, "level": snap.Host.Level, "talking": snap.Host.Talking, "muted": snap.Host.Muted},
		"cohosts":   co,
		"speakers":  sp,
		"listeners": snap.Listeners,
		"listener_list": ll,
		"mute_all":  snap.MuteAll,
		"asset":     snap.Asset,
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
	hostMute := ""
	if snap.Host.Muted {
		hostMute = " 🔇"
	}
	fmt.Fprintf(&b, "  host      %s  lv=%.2f%s\n", emptyDash(snap.Host.Nick), snap.Host.Level, hostMute)
	for i, c := range snap.Cohosts {
		m := ""
		if c.Muted {
			m = " 🔇"
		}
		fmt.Fprintf(&b, "  cohost[%d] %s  lv=%.2f%s\n", i, emptyDash(c.Nick), c.Level, m)
	}
	activeSp := 0
	mutedSp := 0
	for _, c := range snap.Speakers {
		if c.Nick != "" {
			activeSp++
		}
		if c.Muted {
			mutedSp++
		}
	}
	fmt.Fprintf(&b, "  speakers  %d/%d seated · %d muted\n", activeSp, SpaceSpeakerSlots, mutedSp)
	fmt.Fprintf(&b, "  mute-all  %v\n", snap.MuteAll)
	fmt.Fprintf(&b, "  listeners %d\n", snap.Listeners)
	for i, l := range snap.ListenerList {
		if i >= 12 {
			fmt.Fprintf(&b, "    … +%d more\n", len(snap.ListenerList)-12)
			break
		}
		fmt.Fprintf(&b, "    · %s\n", l.Nick)
	}
	fmt.Fprintf(&b, "  rtmp      %s\n", snap.RTMP.BaseRTMPURL())
	fmt.Fprintf(&b, "  key       %s\n", rtmpKeyStatus(snap.RTMP))
	if snap.KeySource != "" {
		fmt.Fprintf(&b, "  key_src   %s\n", snap.KeySource)
	}
	fmt.Fprintf(&b, "  status    %s\n", snap.RTMP.Status)
	if snap.Asset.Offer {
		fmt.Fprintf(&b, "  asset     OFFER · %s · public=%v · guests=%d\n",
			emptyDash(snap.Asset.Operator), snap.Asset.Public, len(snap.Asset.Guests))
	} else {
		fmt.Fprintf(&b, "  asset     off · gy stream-x offer\n")
	}
	if snap.PushID != "" {
		fmt.Fprintf(&b, "  push      id=%s\n", snap.PushID)
	}
	fmt.Fprintf(&b, "  slots     host + %d cohosts + %d speakers + listeners\n", SpaceCohostSlots, SpaceSpeakerSlots)
	fmt.Fprintf(&b, "  burst     site/burst.html · mute · listeners · key pull\n")
	fmt.Fprintf(&b, "  asset cli gy stream-x init|start|offer  ·  gy space key --pull\n")
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
	src := "flag"
	if k == "" {
		if s, key, err := PullStreamKey(PullKeyOpts{}); err == nil && key != "" {
			k, src = key, s
		}
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
	Spaces().SetStreamKeyFrom(k, src)
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
		if rest == "" || rest == "status" {
			fmt.Println(rtmpKeyStatus(Spaces().Snapshot().RTMP))
			if Spaces().Snapshot().KeySource != "" {
				fmt.Println("source:", Spaces().Snapshot().KeySource)
			}
			return nil
		}
		if rest == "--pull" || rest == "pull" || rest == "--auto" {
			src, key, err := PullStreamKey(PullKeyOpts{Clipboard: true})
			if err != nil {
				return err
			}
			Spaces().SetStreamKeyFrom(key, src)
			fmt.Println("stream key auto-pulled ·", src, "·", rtmpKeyStatus(Spaces().Snapshot().RTMP))
			return nil
		}
		Spaces().SetStreamKey(rest)
		fmt.Println("stream key set ·", rtmpKeyStatus(Spaces().Snapshot().RTMP))
	case "key-watch", "watch-key":
		path := rest
		if path == "" {
			path = DefaultStreamKeyPath()
		}
		fmt.Println("watching", path, "· Ctrl+C stop")
		return WatchStreamKeyFile(path, 2*time.Second, func(src, key string) {
			Spaces().SetStreamKeyFrom(key, src)
			fmt.Println("key ready ·", src, "·", rtmpKeyStatus(Spaces().Snapshot().RTMP))
		})
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
		// gy space listeners [list|N|add nick|rm nick]
		parts := strings.Fields(rest)
		if len(parts) == 0 || parts[0] == "list" {
			snap := Spaces().Snapshot()
			fmt.Printf("listeners %d\n", snap.Listeners)
			for _, l := range snap.ListenerList {
				fmt.Println(" ·", l.Nick)
			}
			return nil
		}
		if parts[0] == "add" && len(parts) > 1 {
			Spaces().AddListener(strings.Join(parts[1:], " "), "")
			fmt.Println("listener +", strings.Join(parts[1:], " "))
			return nil
		}
		if (parts[0] == "rm" || parts[0] == "remove") && len(parts) > 1 {
			Spaces().RemoveListener(strings.Join(parts[1:], " "))
			fmt.Println("listener -", strings.Join(parts[1:], " "))
			return nil
		}
		n, _ := strconv.Atoi(parts[0])
		Spaces().SetListeners(n)
		fmt.Println("listeners →", n)
	case "mute":
		// gy space mute host|cohost:N|speaker:N|all [off]
		parts := strings.Fields(rest)
		if len(parts) == 0 {
			return fmt.Errorf("usage: gy space mute host|cohost:N|speaker:N|all [off]")
		}
		off := len(parts) > 1 && (parts[1] == "off" || parts[1] == "0" || parts[1] == "unmute")
		if parts[0] == "all" {
			Spaces().SetMuteAll(!off, "cli")
			fmt.Println("mute-all →", !off)
			return nil
		}
		role, idx, err := parseSeatSpec(parts[0])
		if err != nil {
			return err
		}
		if err := Spaces().SetMute(role, idx, !off, "cli"); err != nil {
			return err
		}
		fmt.Printf("mute %s[%d] → %v\n", role, idx, !off)
	case "unmute":
		parts := strings.Fields(rest)
		if len(parts) == 0 {
			return fmt.Errorf("usage: gy space unmute host|cohost:N|speaker:N|all")
		}
		if parts[0] == "all" {
			Spaces().SetMuteAll(false, "cli")
			fmt.Println("mute-all → false")
			return nil
		}
		role, idx, err := parseSeatSpec(parts[0])
		if err != nil {
			return err
		}
		_ = Spaces().SetMute(role, idx, false, "cli")
		fmt.Printf("unmute %s[%d]\n", role, idx)
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
	return `gy space — X Spaces stage + RTMP producer + stream asset

  gy space                         status (id, roster, mutes, rtmp, asset)
  gy space id <url|id>             bind Space (default 1AJEmmANrPeJL)
  gy space key <stream_key>        set Media Studio RTMP key
  gy space key --pull              auto-pull (env|file|json|clipboard|url)
  gy space key-watch [path]        poll key file until ready
  gy space rtmp | rtmps            select ca.pscp.tv ingest
  gy space caption <text>          lower-third / Space caption
  gy space seat host <nick>
  gy space seat cohost:0 <nick>
  gy space seat speaker:3 <nick>
  gy space mute cohost:0|speaker:3|all
  gy space unmute all
  gy space listeners list|add <n>|rm <n>|<count>

  gy space-rtmp --key KEY --in SRC [--rtmps|--rtmp] [--dry-run]
  gy stream-x init|start|offer     gy as X.com streaming asset for others

  Key auto-pull order:
    GY_X_STREAM_KEY → GY_X_STREAM_KEY_FILE → ~/.config/grokytalky/x-stream-key
    → x-rtmp.json → GY_X_STREAM_KEY_URL → clipboard (pull only)

  RTMPS  rtmps://ca.pscp.tv:443/x/<key>
  RTMP   rtmp://ca.pscp.tv:80/x/<key>
  hub    GET /api/space  ·  GET /api/space/key?token=$GY_SPACE_TOKEN

  burst page: site/burst.html  (mute · listeners · key pull · asset)
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
		if ma, ok := msg["mute_all"].(bool); ok {
			by, _ := msg["from"].(string)
			s.SetMuteAll(ma, by)
		}
		if h, ok := msg["host"].(map[string]any); ok {
			applySlotMesh(s, SpaceRoleHost, 0, h)
		}
		if arr, ok := msg["cohosts"].([]any); ok {
			for _, it := range arr {
				m, _ := it.(map[string]any)
				if m == nil {
					continue
				}
				applySlotMesh(s, SpaceRoleCohost, int(asFloat(m["index"])), m)
			}
		}
		if arr, ok := msg["speakers"].([]any); ok {
			for _, it := range arr {
				m, _ := it.(map[string]any)
				if m == nil {
					continue
				}
				applySlotMesh(s, SpaceRoleSpeaker, int(asFloat(m["index"])), m)
			}
		}
		if arr, ok := msg["listener_list"].([]any); ok {
			for _, it := range arr {
				m, _ := it.(map[string]any)
				if m == nil {
					continue
				}
				if n, _ := m["nick"].(string); n != "" {
					s.AddListener(n, fmt.Sprint(m["id"]))
				}
			}
		} else if n, ok := msg["listeners"].(float64); ok {
			s.SetListeners(int(n))
		}
		if a, ok := msg["asset"].(map[string]any); ok {
			offer, _ := a["offer"].(bool)
			op, _ := a["operator"].(string)
			lab, _ := a["label"].(string)
			pub, _ := a["public"].(bool)
			s.SetAssetOffer(offer, op, lab, pub)
		}
	case "space-level":
		role := SpaceRole(strings.ToLower(fmt.Sprint(msg["role"])))
		idx := int(asFloat(msg["slot"]))
		lv := asFloat(msg["level"])
		s.SetLevel(role, idx, lv)
	case "space-mute":
		role := SpaceRole(strings.ToLower(fmt.Sprint(msg["role"])))
		idx := int(asFloat(msg["slot"]))
		muted, _ := msg["muted"].(bool)
		by, _ := msg["by"].(string)
		if by == "" {
			by, _ = msg["from"].(string)
		}
		_ = s.SetMute(role, idx, muted, by)
	case "space-mute-all":
		muted, _ := msg["muted"].(bool)
		by, _ := msg["by"].(string)
		if by == "" {
			by, _ = msg["from"].(string)
		}
		s.SetMuteAll(muted, by)
	case "space-listener-join":
		nick, _ := msg["nick"].(string)
		if nick == "" {
			nick, _ = msg["from"].(string)
		}
		id, _ := msg["id"].(string)
		s.AddListener(nick, id)
	case "space-listener-leave":
		nick, _ := msg["nick"].(string)
		if nick == "" {
			nick, _ = msg["from"].(string)
		}
		s.RemoveListener(nick)
	case "space-asset":
		offer, _ := msg["offer"].(bool)
		op, _ := msg["operator"].(string)
		if op == "" {
			op, _ = msg["from"].(string)
		}
		lab, _ := msg["label"].(string)
		pub, _ := msg["public"].(bool)
		s.SetAssetOffer(offer, op, lab, pub)
		if guests, ok := msg["guests"].([]any); ok {
			for _, g := range guests {
				if n, ok := g.(string); ok {
					s.AllowGuest(n)
				}
			}
		}
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

func applySlotMesh(s *SpaceState, role SpaceRole, idx int, m map[string]any) {
	if n, _ := m["nick"].(string); n != "" {
		_ = s.Seat(role, idx, n)
	}
	if lv, ok := m["level"].(float64); ok {
		s.SetLevel(role, idx, lv)
	}
	if muted, ok := m["muted"].(bool); ok {
		by, _ := m["muted_by"].(string)
		_ = s.SetMute(role, idx, muted, by)
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
