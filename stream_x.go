package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// stream_x.go — gy as a reusable X.com / Media Studio streaming asset.
// Other creators run `gy stream-x` to init keys, offer seats, and publish
// RTMP(S) to ca.pscp.tv without building their own encoder stack.

// runStreamXCmd: gy stream-x [init|start|status|offer|guest|help]
func runStreamXCmd(args []string) error {
	if len(args) == 0 {
		return runStreamXStatus()
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]
	switch sub {
	case "init", "setup":
		return runStreamXInit(rest)
	case "start", "go", "live":
		return runStreamXStart(rest)
	case "status", "show":
		return runStreamXStatus()
	case "offer", "asset":
		return runStreamXOffer(rest)
	case "guest", "allow":
		if len(rest) == 0 {
			return fmt.Errorf("usage: gy stream-x guest <nick>")
		}
		Spaces().AllowGuest(strings.Join(rest, " "))
		fmt.Println("guest allowed ·", strings.Join(rest, " "))
		return nil
	case "key", "pull":
		src, key, err := PullStreamKey(PullKeyOpts{Clipboard: true})
		if err != nil {
			return err
		}
		Spaces().SetStreamKeyFrom(key, src)
		fmt.Println("stream-x · key from", src, "·", rtmpKeyStatus(Spaces().Snapshot().RTMP))
		return nil
	case "help", "-h", "--help":
		fmt.Print(streamXHelp())
		return nil
	default:
		return fmt.Errorf("unknown stream-x %q\n%s", sub, streamXHelp())
	}
}

func runStreamXStatus() error {
	fmt.Println("stream-x · gy as X.com Media Studio asset")
	fmt.Print(FormatSpaceDoctor(Spaces()))
	fmt.Println()
	fmt.Println("paths")
	fmt.Println("  key file ", DefaultStreamKeyPath())
	fmt.Println("  json     ", DefaultStreamKeyJSONPath())
	fmt.Println("  profile  ", streamXProfilePath())
	if t := SpaceToken(); t != "" {
		fmt.Println("  token    set (GY_SPACE_TOKEN) · hub /api/space/key")
	} else {
		fmt.Println("  token    unset · set GY_SPACE_TOKEN to enable remote key API")
	}
	return nil
}

func runStreamXInit(args []string) error {
	// optional: gy stream-x init --key KEY --space URL --label "…"
	fs := newBridgeFlagSet("stream-x-init")
	key := fs.String("key", "", "optional stream key to store now")
	space := fs.String("space", "1AJEmmANrPeJL", "Space id or URL")
	label := fs.String("label", "GrokYtalkY stream asset", "asset label")
	secure := fs.Bool("rtmps", true, "prefer RTMPS")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := filepath.Dir(DefaultStreamKeyPath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// profile
	prof := fmt.Sprintf(`# GrokYtalkY stream-x profile — X.com Media Studio asset
# Generated %s
space = %q
label = %q
secure = %v
rtmp = %q
rtmps = %q
key_file = %q
# Paste stream key into key_file (chmod 600) when Media Studio is ready.
# Or: export GY_X_STREAM_KEY=…
# Or: gy space key --pull
`,
		time.Now().UTC().Format(time.RFC3339),
		NormalizeSpaceID(*space),
		*label,
		*secure,
		XRTMPURL,
		XRTMPSURL,
		DefaultStreamKeyPath(),
	)
	if err := os.WriteFile(streamXProfilePath(), []byte(prof), 0o600); err != nil {
		return err
	}
	// empty key file placeholder
	kp := DefaultStreamKeyPath()
	if _, err := os.Stat(kp); os.IsNotExist(err) {
		_ = os.WriteFile(kp, []byte("# paste X Media Studio RTMP stream key here (one line)\n"), 0o600)
	}
	// json skeleton
	jp := DefaultStreamKeyJSONPath()
	if _, err := os.Stat(jp); os.IsNotExist(err) {
		_ = os.WriteFile(jp, []byte(`{
  "stream_key": "",
  "secure": true,
  "base_url": "rtmps://ca.pscp.tv:443/x",
  "note": "stream key available when ready — fill from studio.x.com Sources"
}
`), 0o600)
	}
	Spaces().SetID(*space)
	Spaces().SetSecure(*secure)
	Spaces().SetAssetOffer(false, "", *label, false)
	if k := strings.TrimSpace(*key); k != "" {
		path, err := WriteStreamKeyFile(k)
		if err != nil {
			return err
		}
		Spaces().SetStreamKeyFrom(k, "file:"+path)
		fmt.Println("stream-x · key stored ·", path)
	}
	fmt.Println("stream-x · initialized")
	fmt.Println("  profile ", streamXProfilePath())
	fmt.Println("  key     ", DefaultStreamKeyPath(), "  ← paste when ready")
	fmt.Println("  json    ", DefaultStreamKeyJSONPath())
	fmt.Println("  next    gy stream-x key   # auto-pull")
	fmt.Println("          gy stream-x offer --public")
	fmt.Println("          gy stream-x start --in clip.mp4")
	fmt.Println("  others  gy stream-x guest <nick>  # allow peer to use this asset")
	return nil
}

func runStreamXStart(args []string) error {
	fs := newBridgeFlagSet("stream-x-start")
	in := fs.String("in", "", "ffmpeg input")
	key := fs.String("key", "", "override stream key")
	pull := fs.Bool("pull", true, "auto-pull key before start")
	dry := fs.Bool("dry-run", false, "print only")
	plain := fs.Bool("rtmp", false, "use plain rtmp://")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pull || strings.TrimSpace(*key) == "" {
		src, k, err := PullStreamKey(PullKeyOpts{Clipboard: true})
		if err == nil && k != "" {
			Spaces().SetStreamKeyFrom(k, src)
			fmt.Println("stream-x · key ·", src)
		}
	}
	if k := strings.TrimSpace(*key); k != "" {
		Spaces().SetStreamKeyFrom(k, "flag")
	}
	if *plain {
		Spaces().SetSecure(false)
	}
	cfg := Spaces().Snapshot().RTMP
	if *dry || strings.TrimSpace(*in) == "" {
		// reuse space-rtmp dry path
		args2 := []string{"--dry-run"}
		if cfg.StreamKey != "" {
			args2 = append(args2, "--key", cfg.StreamKey)
		}
		if *plain {
			args2 = append(args2, "--rtmp")
		}
		if *in != "" {
			args2 = append(args2, "--in", *in)
		}
		return runSpaceRTMPCmd(args2)
	}
	id, err := StartSpaceRTMPPush(*in, cfg)
	if err != nil {
		return fmt.Errorf("stream-x start: %w\n  tip: gy stream-x key  ·  paste key into %s", err, DefaultStreamKeyPath())
	}
	Spaces().mu.Lock()
	Spaces().PushID = id
	Spaces().mu.Unlock()
	fmt.Printf("stream-x · LIVE id=%s → %s/…\n", id, cfg.BaseRTMPURL())
	fmt.Println("  asset ready for guests · gy stream-x offer --public")
	return nil
}

func runStreamXOffer(args []string) error {
	fs := newBridgeFlagSet("stream-x-offer")
	off := fs.Bool("off", false, "stop offering")
	pub := fs.Bool("public", false, "announce to whole room")
	label := fs.String("label", "", "asset label")
	op := fs.String("operator", "", "operator nick")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// also accept bare "off"
	for _, a := range args {
		if a == "off" || a == "stop" {
			*off = true
		}
		if a == "public" {
			*pub = true
		}
	}
	Spaces().SetAssetOffer(!*off, *op, *label, *pub)
	snap := Spaces().Snapshot()
	fmt.Printf("stream-x · asset offer=%v public=%v label=%q\n",
		snap.Asset.Offer, snap.Asset.Public, snap.Asset.Label)
	fmt.Println("  mesh: type space-asset · hub GET /api/space")
	fmt.Println("  guests: gy stream-x guest <nick>")
	return nil
}

func streamXProfilePath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return "stream-x.toml"
	}
	return filepath.Join(home, ".config", "grokytalky", "stream-x.toml")
}

func streamXHelp() string {
	return `gy stream-x — use GrokYtalkY as an X.com streaming asset

Anyone can leverage gy to encode + publish to X Media Studio (Periscope RTMP):

  gy stream-x init                 scaffold key file + profile
  gy stream-x key                  auto-pull stream key (env/file/json/clipboard/url)
  gy stream-x start --in SRC       pull key + ffmpeg → ca.pscp.tv
  gy stream-x offer --public       advertise this gy as room stream asset
  gy stream-x guest alice          allow alice to seat / request publish
  gy stream-x status

Other users (guests):
  1. Join same gy hub room as the asset operator
  2. Seat via /space seat speaker:N me  (or burst.html)
  3. Operator runs gy stream-x start --in …  (shared RTMP egress)
  4. Host mutes: /space mute speaker:3 · /space mute all

Key when ready:
  echo "$KEY" > ~/.config/grokytalky/x-stream-key && chmod 600 …
  export GY_X_STREAM_KEY=…
  export GY_X_STREAM_KEY_URL=https://vault.example/x-key
  export GY_SPACE_TOKEN=…          # protects GET /api/space/key

Ingest:
  rtmps://ca.pscp.tv:443/x/<key>
  rtmp://ca.pscp.tv:80/x/<key>

See docs/stream-x.md · site/burst.html
`
}
