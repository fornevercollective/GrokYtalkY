package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// NMOS IS-04 / IS-05 scaffolding for facility discovery & connection.
// gy does not run a full registry or act as a production GM — it can
// *advertise* senders (2110 essences + mid-lane) and *POST* registrations
// when GY_NMOS_REGISTRY is set. Connection management (IS-05) is documented
// and stub-activated for tooling tests.

const (
	NMOSIS04Version = "v1.3"
	NMOSIS05Version = "v1.1"
)

// NMOSNode is an IS-04 node resource (this process).
type NMOSNode struct {
	ID          string            `json:"id"`
	Version     string            `json:"version"`
	Label       string            `json:"label"`
	Description string            `json:"description"`
	Href        string            `json:"href"`
	Hostname    string            `json:"hostname,omitempty"`
	Caps        map[string]any    `json:"caps,omitempty"`
	Services    []NMOSService     `json:"services,omitempty"`
	API         map[string]string `json:"api,omitempty"`
	Clocks      []NMOSClock       `json:"clocks,omitempty"`
	Interfaces  []NMOSInterface   `json:"interfaces,omitempty"`
}

// NMOSService is a node-attached control service (IS-05 connection API).
type NMOSService struct {
	Href string `json:"href"`
	Type string `json:"type"` // urn:x-nmos:service:connection/v1.1
}

// NMOSClock describes a node clock (PTP domain when locked).
type NMOSClock struct {
	Name    string `json:"name"`
	RefType string `json:"ref_type"` // ptp | internal
	Version string `json:"version,omitempty"`
	Gmid    string `json:"gmid,omitempty"`
	Locked  bool   `json:"locked,omitempty"`
	Trace   bool   `json:"traceable,omitempty"`
}

// NMOSInterface is a network interface for essence.
type NMOSInterface struct {
	Name      string `json:"name"`
	ChassisID string `json:"chassis_id,omitempty"`
	PortID    string `json:"port_id,omitempty"`
}

// NMOSSender is an IS-04 sender (one essence / mid-lane).
type NMOSSender struct {
	ID          string         `json:"id"`
	Version     string         `json:"version"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	FlowID      string         `json:"flow_id"`
	Transport   string         `json:"transport"` // urn:x-nmos:transport:rtp
	DeviceID    string         `json:"device_id"`
	Manifest    string         `json:"manifest_href,omitempty"`
	Interface   []string       `json:"interface_bindings,omitempty"`
	Tags        map[string]any `json:"tags,omitempty"`
}

// NMOSFlow is an IS-04 flow (codec / format).
type NMOSFlow struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Label       string `json:"label"`
	Description string `json:"description"`
	SourceID    string `json:"source_id"`
	DeviceID    string `json:"device_id"`
	Format      string `json:"format"` // urn:x-nmos:format:video|audio|data
	MediaType   string `json:"media_type,omitempty"`
}

// NMOSDevice groups senders on this host.
type NMOSDevice struct {
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	NodeID      string   `json:"node_id"`
	Senders     []string `json:"senders"`
	Receivers   []string `json:"receivers"`
	Controls    []any    `json:"controls,omitempty"`
}

// NMOSResourceBundle is the full IS-04 advertisement snapshot for this gy process.
type NMOSResourceBundle struct {
	Node    NMOSNode     `json:"node"`
	Devices []NMOSDevice `json:"devices"`
	Sources []map[string]any `json:"sources,omitempty"`
	Flows   []NMOSFlow   `json:"flows"`
	Senders []NMOSSender `json:"senders"`
	// facility
	Registry string         `json:"registry,omitempty"`
	PTP      PTPStatus      `json:"ptp"`
	Gaps     []string       `json:"gaps,omitempty"`
	T        int64          `json:"t"`
	Note     string         `json:"note"`
}

// DefaultNMOSNode builds an IS-04 node for this host from env + sync report.
func DefaultNMOSNode(baseHref string) NMOSNode {
	if baseHref == "" {
		baseHref = firstNonEmpty(os.Getenv("GY_NMOS_HREF"), "http://127.0.0.1:9876/")
	}
	host, _ := os.Hostname()
	id := firstNonEmpty(os.Getenv("GY_NMOS_NODE_ID"), "urn:uuid:gy-node-"+randID(4))
	sync := SyncClockFromEnv()
	clk := NMOSClock{Name: "clk0", RefType: "internal", Locked: false}
	if sync.PTP.Mode == PTPLocked || sync.PTP.Mode == PTPSlave {
		clk = NMOSClock{
			Name:    "clk0",
			RefType: "ptp",
			Version: "IEEE1588-2008",
			Locked:  sync.PTP.Mode == PTPLocked,
			Trace:   sync.PTP.Traceable,
		}
	}
	return NMOSNode{
		ID:          id,
		Version:     time.Now().UTC().Format("2006-01-02T15:04:05.000000Z"),
		Label:       firstNonEmpty(os.Getenv("GY_NMOS_LABEL"), "GrokYtalkY venue"),
		Description: "DOJO / venue sender · ST 2110 essences + mid-lane (gy)",
		Href:        strings.TrimRight(baseHref, "/") + "/",
		Hostname:    host,
		Caps: map[string]any{
			"urn:x-nmos:cap:versions": []string{NMOSIS04Version},
		},
		Services: []NMOSService{
			{
				Href: strings.TrimRight(baseHref, "/") + "/x-nmos/connection/" + NMOSIS05Version + "/",
				Type: "urn:x-nmos:service:connection/" + NMOSIS05Version,
			},
		},
		API: map[string]string{
			"versions": NMOSIS04Version,
		},
		Clocks: []NMOSClock{clk},
		Interfaces: []NMOSInterface{
			{Name: firstNonEmpty(os.Getenv("GY_PTP_IFACE"), "eth0")},
		},
	}
}

// BuildNMOSResources constructs senders for 2110-20/30/40 + mid-lane (software).
func BuildNMOSResources() NMOSResourceBundle {
	node := DefaultNMOSNode("")
	devID := firstNonEmpty(os.Getenv("GY_NMOS_DEVICE_ID"), "urn:uuid:gy-device-"+randID(4))
	sync := SyncClockFromEnv()

	flows := []NMOSFlow{
		{
			ID: "urn:uuid:gy-flow-video", Version: node.Version, Label: "program-video",
			Description: "ST 2110-20 program (or lab H.264)", SourceID: "urn:uuid:gy-src-video",
			DeviceID: devID, Format: "urn:x-nmos:format:video", MediaType: "video/raw",
		},
		{
			ID: "urn:uuid:gy-flow-audio", Version: node.Version, Label: "program-audio",
			Description: "ST 2110-30 L24", SourceID: "urn:uuid:gy-src-audio",
			DeviceID: devID, Format: "urn:x-nmos:format:audio", MediaType: "audio/L24",
		},
		{
			ID: "urn:uuid:gy-flow-anc", Version: node.Version, Label: "program-anc",
			Description: "ST 2110-40 DID 0x5F mark/tally/caption", SourceID: "urn:uuid:gy-src-anc",
			DeviceID: devID, Format: "urn:x-nmos:format:data", MediaType: "video/smpte291",
		},
		{
			ID: "urn:uuid:gy-flow-mid", Version: node.Version, Label: "mid-lane",
			Description: "Edge mid-lane program+hexlum (not essence RTP)", SourceID: "urn:uuid:gy-src-mid",
			DeviceID: devID, Format: "urn:x-nmos:format:data", MediaType: "application/json",
		},
	}

	senders := []NMOSSender{
		{
			ID: "urn:uuid:gy-sender-video", Version: node.Version, Label: "2110-20",
			Description: "Program video RTP", FlowID: flows[0].ID, DeviceID: devID,
			Transport: "urn:x-nmos:transport:rtp.mcast",
			Manifest:  firstNonEmpty(os.Getenv("GY_NMOS_VIDEO_SDP"), ""),
			Tags:      map[string]any{"gy.essence": ST2110_20},
		},
		{
			ID: "urn:uuid:gy-sender-audio", Version: node.Version, Label: "2110-30",
			Description: "Program audio RTP", FlowID: flows[1].ID, DeviceID: devID,
			Transport: "urn:x-nmos:transport:rtp.mcast",
			Tags:      map[string]any{"gy.essence": ST2110_30},
		},
		{
			ID: "urn:uuid:gy-sender-anc", Version: node.Version, Label: "2110-40",
			Description: "ANC RTP", FlowID: flows[2].ID, DeviceID: devID,
			Transport: "urn:x-nmos:transport:rtp.mcast",
			Tags:      map[string]any{"gy.essence": ST2110_40},
		},
		{
			ID: "urn:uuid:gy-sender-mid", Version: node.Version, Label: "mid-lane",
			Description: "Public mid-lane edge (gy mid-lane → CF)", FlowID: flows[3].ID, DeviceID: devID,
			Transport: "urn:x-nmos:transport:websocket", // logical; not SMPTE essence
			Tags:      map[string]any{"gy.lane": "mid", "gy.edge": true},
		},
	}

	dev := NMOSDevice{
		ID: devID, Version: node.Version, Label: "gy-venue",
		Type: "urn:x-nmos:device:generic", NodeID: node.ID,
		Senders: []string{
			senders[0].ID, senders[1].ID, senders[2].ID, senders[3].ID,
		},
		Receivers: []string{},
	}

	gaps := []string{}
	if sync.PTP.Mode == PTPFreeRun {
		gaps = append(gaps, "PTP free-run — set GY_PTP_LOCKED=1 + domain/offset when GM attached")
	}
	reg := strings.TrimSpace(os.Getenv("GY_NMOS_REGISTRY"))
	if reg == "" {
		gaps = append(gaps, "GY_NMOS_REGISTRY unset — IS-04 registration not posted (facility registry)")
	}
	gaps = append(gaps, "IS-05 connection management is stub (activate via facility controller)")

	return NMOSResourceBundle{
		Node:     node,
		Devices:  []NMOSDevice{dev},
		Flows:    flows,
		Senders:  senders,
		Registry: reg,
		PTP:      sync.PTP,
		Gaps:     gaps,
		T:        time.Now().UnixMilli(),
		Note:     "NMOS scaffold — honest non-registry until GY_NMOS_REGISTRY posts resources",
	}
}

// FormatNMOSReport multi-line for gy doctor nmos.
func FormatNMOSReport(b NMOSResourceBundle) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "NMOS IS-04/%s · IS-05/%s scaffold\n", NMOSIS04Version, NMOSIS05Version)
	fmt.Fprintf(&sb, "  node %s · %s\n", truncate(b.Node.ID, 40), b.Node.Label)
	fmt.Fprintf(&sb, "  href %s\n", b.Node.Href)
	fmt.Fprintf(&sb, "  ptp mode=%s domain=%d locked=%v\n",
		b.PTP.Mode, b.PTP.Domain, b.PTP.Mode == PTPLocked)
	fmt.Fprintf(&sb, "  devices %d · flows %d · senders %d\n",
		len(b.Devices), len(b.Flows), len(b.Senders))
	for _, s := range b.Senders {
		fmt.Fprintf(&sb, "  sender %-12s  %s\n", s.Label, truncate(s.ID, 36))
	}
	if b.Registry != "" {
		fmt.Fprintf(&sb, "  registry %s\n", b.Registry)
	} else {
		fmt.Fprintf(&sb, "  registry (unset)\n")
	}
	for _, g := range b.Gaps {
		fmt.Fprintf(&sb, "  gap: %s\n", g)
	}
	fmt.Fprintf(&sb, "  note: %s\n", b.Note)
	return sb.String()
}

// PostNMOSRegistration POSTs node+devices+senders JSON to a registry root if configured.
// Best-effort; never required for gy serve.
func PostNMOSRegistration(bundle NMOSResourceBundle) error {
	reg := strings.TrimSpace(bundle.Registry)
	if reg == "" {
		return fmt.Errorf("GY_NMOS_REGISTRY not set")
	}
	reg = strings.TrimRight(reg, "/")
	client := &http.Client{Timeout: 8 * time.Second}
	// Many registries accept bulk under /x-nmos/registration/v1.3/resource
	url := reg + "/x-nmos/registration/" + NMOSIS04Version + "/resource"
	body, _ := json.Marshal(map[string]any{
		"type":  "node",
		"data":  bundle.Node,
		"gy":    true,
		"bundle": bundle,
	})
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GrokYtalkY-nmos/"+Version)
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode >= 300 {
		return fmt.Errorf("registry HTTP %s", res.Status)
	}
	return nil
}

// SyncClockFromEnv returns free-run or locked report from facility env.
//
//	GY_PTP_LOCKED=1|true
//	GY_PTP_DOMAIN=127
//	GY_PTP_OFFSET_NS=0
//	GY_PTP_IFACE=eth0
//	GY_PTP_MODE=free-run|slave|locked|master
func SyncClockFromEnv() SyncClockReport {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("GY_PTP_MODE")))
	locked := envTruthy("GY_PTP_LOCKED")
	domain := 127
	if v := strings.TrimSpace(os.Getenv("GY_PTP_DOMAIN")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			domain = n
		}
	}
	var offset int64
	if v := strings.TrimSpace(os.Getenv("GY_PTP_OFFSET_NS")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			offset = n
		}
	}
	iface := strings.TrimSpace(os.Getenv("GY_PTP_IFACE"))

	if locked || mode == "locked" {
		return SyncClockWithPTPLocked(domain, offset, iface)
	}
	if mode == "slave" {
		r := DefaultSyncClockReport()
		r.PTP.Mode = PTPSlave
		r.PTP.Domain = domain
		r.PTP.Interface = iface
		r.PTP.OffsetNs = offset
		r.PTP.Note = "PTP slave (not locked) — waiting for GM / BC"
		r.Gaps = append([]string{"PTP slave not yet locked"}, r.Gaps...)
		// remove duplicate free-run gap if present
		r.Updated = time.Now().UnixMilli()
		return r
	}
	if mode == "master" {
		r := DefaultSyncClockReport()
		r.PTP.Mode = PTPMaster
		r.PTP.Domain = domain
		r.PTP.Interface = iface
		r.PTP.Note = "PTP master/GM mode signaled — gy does not implement full GM stack"
		r.Gaps = []string{"software GM not provided — use facility grandmaster"}
		r.Compliant = false
		r.Updated = time.Now().UnixMilli()
		return r
	}
	return DefaultSyncClockReport()
}

func envTruthy(k string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(k)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// firstNonEmpty is used elsewhere — if already defined, this will conflict.
// Check: kindFromName had firstNonEmpty - grep