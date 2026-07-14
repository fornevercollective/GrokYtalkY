package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// runBitChatCmd: gy bitchat [doctor|bridge|send|sim|help]
func runBitChatCmd(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
		args = args[1:]
	}
	switch sub {
	case "", "doctor", "status", "info":
		fmt.Print(FormatBitChatDoctor())
		fmt.Println()
		fmt.Println("usage:")
		fmt.Println("  gy bitchat doctor")
		fmt.Println("  gy bitchat send \"hello offline\" [--room R] [--from nick]")
		fmt.Println("  gy bitchat sim [text] [--from alice] [--n 3]")
		fmt.Println("  gy bitchat bridge [--hub ws://127.0.0.1:9876]   # adapter loop (poll egress + log)")
		fmt.Println("  gy bitchat help")
		fmt.Println()
		fmt.Println("native: https://github.com/permissionlesstech/bitchat")
		fmt.Println("        adapter POSTs BLE/Nostr messages → http://HUB/api/bitchat/ingress")
		return nil

	case "help", "-h", "--help":
		fmt.Print(bitChatHelp())
		return nil

	case "send":
		return bitchatSend(args)

	case "sim", "fake", "demo":
		return bitchatSim(args)

	case "bridge", "adapter", "loop":
		return bitchatBridgeLoop(args)

	default:
		// treat as send text
		return bitchatSend(append([]string{sub}, args...))
	}
}

func bitChatHelp() string {
	return `gy bitchat — dual-path offline mesh (BitChat BLE / Nostr)

  BitChat apps talk without Wi‑Fi. GrokYtalkY bridges them into the hub
  so sphere / GrokGlyph / terminal chat stay unified.

  doctor     status · peers · API map
  send       inject chat into hub (+ optional egress queue for BLE)
  sim        simulate a BLE peer (no native app required)
  bridge     long-running adapter stub: polls egress, logs dual-path

  HTTP (hub serve):
    GET  /api/bitchat
    POST /api/bitchat/ingress   {"type":"chat","from":"alice","text":"hi","transport":"ble"}
    GET  /api/bitchat/egress    drain hub→BLE queue for native app
    POST /api/bitchat/send      {"text":"hi","from":"crew","dual":true}
    POST /api/bitchat/sim       {"text":"ping","from":"bob"}

  Mesh WS types: bitchat-chat · bitchat-presence · bitchat-control · chat(meta.via=bitchat)

  env: GY_BITCHAT=0 to disable · GY_BITCHAT_CHANNEL · GY_ROOM

  native:
    https://github.com/permissionlesstech/bitchat
    https://github.com/jackjackbits/bitchat-1
    https://github.com/permissionlesstech/bitchat-android
`
}

func bitchatHubHTTP() string {
	if u := strings.TrimSpace(os.Getenv("GY_HUB_HTTP")); u != "" {
		return strings.TrimRight(u, "/")
	}
	if u := strings.TrimSpace(os.Getenv("GY_BITCHAT_HUB")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://127.0.0.1:9876"
}

func bitchatSend(args []string) error {
	text := ""
	from := "terminal"
	room := ""
	dual := true
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--from" && i+1 < len(args):
			i++
			from = args[i]
		case a == "--room" && i+1 < len(args):
			i++
			room = args[i]
		case a == "--no-dual":
			dual = false
		case a == "--dual":
			dual = true
		case strings.HasPrefix(a, "-"):
			// skip
		default:
			if text != "" {
				text += " "
			}
			text += a
		}
	}
	if text == "" {
		return fmt.Errorf("usage: gy bitchat send \"message\" [--from nick] [--room R]")
	}
	body, _ := json.Marshal(map[string]any{
		"text": text, "from": from, "room": room, "dual": dual,
	})
	url := bitchatHubHTTP() + "/api/bitchat/send"
	res, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		// local bus fallback if hub not up
		fmt.Println("hub unreachable — injecting local bus only:", err)
		return BitChat().Ingress(BitChatEnvelope{
			Type: "chat", Text: text, From: from, Room: room, Transport: "sim",
			T: time.Now().UnixMilli(),
		})
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<10))
	fmt.Println(string(raw))
	if res.StatusCode >= 300 {
		return fmt.Errorf("send HTTP %d", res.StatusCode)
	}
	return nil
}

func bitchatSim(args []string) error {
	text := "hello from simulated BLE mesh"
	from := "alice"
	n := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--from" && i+1 < len(args):
			i++
			from = args[i]
		case a == "--n" && i+1 < len(args):
			i++
			fmt.Sscanf(args[i], "%d", &n)
		case strings.HasPrefix(a, "-"):
		default:
			if text == "hello from simulated BLE mesh" || i == 0 {
				text = a
			} else {
				text += " " + a
			}
		}
	}
	body, _ := json.Marshal(map[string]any{"text": text, "from": from, "n": n})
	url := bitchatHubHTTP() + "/api/bitchat/sim"
	res, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Println("hub unreachable — local sim:", err)
		return BitChat().Ingress(BitChatEnvelope{
			Type: "chat", Text: text, From: from, Transport: "sim",
			Channel: "mesh#bluetooth", T: time.Now().UnixMilli(),
		})
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<10))
	fmt.Println(string(raw))
	return nil
}

// bitchatBridgeLoop is a stub adapter: connect WS as bitchat-bridge, poll egress HTTP,
// log dual-path. Real native BitChat app would replace BLE I/O here.
func bitchatBridgeLoop(args []string) error {
	hubWS := "ws://127.0.0.1:9876/?role=bitchat-bridge&nick=bc-bridge&room=global"
	httpBase := bitchatHubHTTP()
	for i := 0; i < len(args); i++ {
		if (args[i] == "--hub" || args[i] == "-h") && i+1 < len(args) {
			i++
			hubWS = args[i]
			if !strings.Contains(hubWS, "role=") {
				sep := "?"
				if strings.Contains(hubWS, "?") {
					sep = "&"
				}
				hubWS += sep + "role=bitchat-bridge&nick=bc-bridge"
			}
		}
		if args[i] == "--http" && i+1 < len(args) {
			i++
			httpBase = strings.TrimRight(args[i], "/")
		}
	}
	fmt.Println("bitchat bridge stub")
	fmt.Println("  hub  ", hubWS)
	fmt.Println("  http ", httpBase)
	fmt.Println("  poll egress every 2s · Ctrl+C to stop")
	fmt.Println("  native apps should POST BLE messages →", httpBase+"/api/bitchat/ingress")

	// optional WS presence
	go func() {
		// soft connect via client if available — best-effort print
		fmt.Println("  tip: open site with hub connected to see bt: peers after gy bitchat sim")
	}()

	client := &http.Client{Timeout: 10 * time.Second}
	for {
		res, err := client.Get(httpBase + "/api/bitchat/egress?n=16")
		if err != nil {
			fmt.Println("egress poll:", err)
			time.Sleep(3 * time.Second)
			continue
		}
		var out struct {
			OK       bool              `json:"ok"`
			Messages []BitChatEnvelope `json:"messages"`
		}
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		for _, m := range out.Messages {
			fmt.Printf("→ BLE/Nostr egress  from=%s type=%s text=%q room=%s\n",
				m.From, m.Type, truncate(m.Text, 80), m.Room)
		}
		// status line
		st, err := client.Get(httpBase + "/api/bitchat")
		if err == nil {
			var snap map[string]any
			_ = json.NewDecoder(st.Body).Decode(&snap)
			st.Body.Close()
			fmt.Printf("  bridges=%v peers=%v egressq=%v\n",
				snap["bridges"], snap["peer_n"], snap["egress_queued"])
		}
		time.Sleep(2 * time.Second)
	}
}
