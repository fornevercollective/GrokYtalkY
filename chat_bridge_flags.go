package main

import (
	"flag"
	"fmt"
	"os"
)

// bridgeFlagSet wraps flag.FlagSet so chat-bridge has its own Parse without
// colliding with the main CLI FlagSet lifecycle.
type bridgeFlagSet struct {
	*flag.FlagSet
}

func newBridgeFlagSet(name string) *bridgeFlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `gy chat-bridge — thin DOJO hub → public Space caption bridge

  gy chat-bridge [flags]

  --hub     DOJO hub WS (default ws://127.0.0.1:9876/)
  --space   public Space chat WS (wrangler dev default :8787)
  --hosts   comma-separated nicks to mirror (empty = all, dev only)
  --dry-run log only, do not send to Space
  --quiet   less logging

Full flow:
  gy serve
  cd chat/worker && npx wrangler dev
  gy chat-bridge --hosts yournick
  open site/chat.html  (or Pages chat demo)
`)
	}
	return &bridgeFlagSet{FlagSet: fs}
}
