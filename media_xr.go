package main

import (
	"fmt"
	"os"
	"strings"
)

// AR / VR / MR headset & smart-glasses ingest.
// No per-vendor browser plugins — devices enter as UVC/NDI/SRT/WebXR/cast URLs.
//
// Schemes:
//
//	xr:                 auto best headset/glasses on host
//	xr:quest            Meta Quest family
//	xr:vision           Apple Vision Pro
//	xr:hololens         Microsoft HoloLens
//	xr:magicleap        Magic Leap
//	xr:pico             Pico
//	xr:vive             HTC Vive / SteamVR
//	xr:varjo            Varjo
//	xr:xreal            XREAL / Nreal
//	xr:viture           Viture
//	xr:rokid            Rokid
//	xr:spectacles       Snap Spectacles
//	xr:glass            Google Glass Enterprise
//	stereo:sbs:<src>    side-by-side stereo → equirect half map
//	stereo:ou:<src>     over-under stereo
//	stereo:eq:<src>     full equirect 360 (already lat-long)
//	webxr:              browser WebXR passthrough (client-side)
//
// Env:
//
//	GY_XR_CAST_URL      companion cast HLS/SRT (Quest casting PC, etc.)
//	GY_XR_LEFT_URL      left-eye stream
//	GY_XR_RIGHT_URL     right-eye stream

// XRBrand is a known glasses/headset family.
type XRBrand struct {
	ID      string   // scheme suffix: quest, vision, …
	Label   string   // human
	Kind    string   // ar|vr|mr
	Match   []string // substrings in device/label
	Stereo  string   // default layout: mono|sbs|ou|equirect
	Brand   string
	Detail  string
}

// XRCatalog — major AR/VR/MR lines (ingest adapters, not native SDKs).
func XRCatalog() []XRBrand {
	return []XRBrand{
		{ID: "quest", Label: "Meta Quest", Kind: "mr", Brand: "Meta",
			Match: []string{"quest", "oculus", "meta quest"}, Stereo: "sbs",
			Detail: "Quest Link / Air Link cast → NDI/SRT/HLS · or UVC if exposed"},
		{ID: "vision", Label: "Apple Vision Pro", Kind: "mr", Brand: "Apple",
			Match: []string{"vision pro", "apple vision", "reality"}, Stereo: "sbs",
			Detail: "Mac Virtual Display / Continuity · WebXR Safari · external capture"},
		{ID: "hololens", Label: "Microsoft HoloLens", Kind: "mr", Brand: "Microsoft",
			Match: []string{"hololens", "holo lens"}, Stereo: "mono",
			Detail: "Research Mode / PV stream · NDI apps · RTSP"},
		{ID: "magicleap", Label: "Magic Leap", Kind: "ar", Brand: "Magic Leap",
			Match: []string{"magic leap", "magicleap"}, Stereo: "sbs",
			Detail: "Device stream / capture card"},
		{ID: "pico", Label: "Pico", Kind: "vr", Brand: "Pico",
			Match: []string{"pico"}, Stereo: "sbs",
			Detail: "Pico streaming / USB / NDI"},
		{ID: "vive", Label: "HTC Vive / SteamVR", Kind: "vr", Brand: "HTC",
			Match: []string{"vive", "index", "steamvr", "valve"}, Stereo: "sbs",
			Detail: "SteamVR mirror · OpenXR · virtual cam"},
		{ID: "varjo", Label: "Varjo", Kind: "mr", Brand: "Varjo",
			Match: []string{"varjo"}, Stereo: "sbs",
			Detail: "Varjo Base streaming · high-res MR"},
		{ID: "xreal", Label: "XREAL / Nreal", Kind: "ar", Brand: "XREAL",
			Match: []string{"xreal", "nreal", "air 2", "beam"}, Stereo: "sbs",
			Detail: "USB-C display + RGB cam when UVC"},
		{ID: "viture", Label: "Viture", Kind: "ar", Brand: "Viture",
			Match: []string{"viture"}, Stereo: "sbs",
			Detail: "Neckband / USB stream"},
		{ID: "rokid", Label: "Rokid", Kind: "ar", Brand: "Rokid",
			Match: []string{"rokid"}, Stereo: "sbs",
			Detail: "Station / USB / cast"},
		{ID: "spectacles", Label: "Snap Spectacles", Kind: "ar", Brand: "Snap",
			Match: []string{"spectacles", "snap os"}, Stereo: "mono",
			Detail: "Lens Studio / companion export → URL"},
		{ID: "glass", Label: "Google Glass Enterprise", Kind: "ar", Brand: "Google",
			Match: []string{"glass enterprise", "google glass"}, Stereo: "mono",
			Detail: "ADB / RTSP / enterprise management stream"},
		{ID: "openxr", Label: "OpenXR generic", Kind: "xr", Brand: "OpenXR",
			Match: []string{"openxr", "open xr"}, Stereo: "sbs",
			Detail: "Any OpenXR runtime mirror → virtual camera / NDI"},
		{ID: "webxr", Label: "WebXR browser", Kind: "xr", Brand: "WebXR",
			Match: []string{"webxr"}, Stereo: "sbs",
			Detail: "Client navigator.xr session · passthrough when supported"},
	}
}

// ClassifyXRLabel maps a device name to an XR brand (if any).
func ClassifyXRLabel(label string) *XRBrand {
	low := strings.ToLower(label)
	catalog := XRCatalog()
	for i := range catalog {
		for _, m := range catalog[i].Match {
			if strings.Contains(low, m) {
				cp := catalog[i]
				return &cp
			}
		}
	}
	return nil
}

// ListXRSources builds ingest chips for XR + env cast URLs + matched UVC devices.
func ListXRSources() []IngestSource {
	var out []IngestSource

	// Env cast (Quest PC casting, etc.)
	if u := strings.TrimSpace(os.Getenv("GY_XR_CAST_URL")); u != "" {
		out = append(out, IngestSource{
			ID: u, Scheme: "xr-cast", Label: "XR cast URL",
			Detail: u, Ready: true, Brand: "XR", Kind: "mr", Slot: "C",
		})
	}
	if L := strings.TrimSpace(os.Getenv("GY_XR_LEFT_URL")); L != "" {
		out = append(out, IngestSource{
			ID: L, Scheme: "xr-eye", Label: "XR left eye",
			Detail: "GY_XR_LEFT_URL", Ready: true, Brand: "XR", Kind: "stereo-L", Slot: "L1",
		})
	}
	if R := strings.TrimSpace(os.Getenv("GY_XR_RIGHT_URL")); R != "" {
		out = append(out, IngestSource{
			ID: R, Scheme: "xr-eye", Label: "XR right eye",
			Detail: "GY_XR_RIGHT_URL", Ready: true, Brand: "XR", Kind: "stereo-R", Slot: "R1",
		})
	}

	// Catalog entries (always listed; ready if env or matching device)
	devices := listLocalVideoDevices()
	for _, brand := range XRCatalog() {
		ready := false
		detail := brand.Detail
		id := "xr:" + brand.ID
		// match physical device
		for _, d := range devices {
			if b := ClassifyXRLabel(d.Label); b != nil && b.ID == brand.ID {
				ready = true
				id = d.ID
				detail = d.Label + " · " + brand.Detail
				break
			}
		}
		if brand.ID == "webxr" {
			ready = true // client-side
			id = "webxr:"
		}
		if brand.ID == "quest" && strings.TrimSpace(os.Getenv("GY_XR_CAST_URL")) != "" {
			ready = true
			id = strings.TrimSpace(os.Getenv("GY_XR_CAST_URL"))
		}
		out = append(out, IngestSource{
			ID:     id,
			Scheme: "xr",
			Label:  brand.Label,
			Detail: detail + " · stereo=" + brand.Stereo,
			Ready:  ready,
			Brand:  brand.Brand,
			Kind:   brand.Kind,
			Slot:   "C",
		})
	}

	// Stereo helpers
	out = append(out,
		IngestSource{
			ID: "stereo:sbs:", Scheme: "stereo", Label: "Stereo SBS (side-by-side)",
			Detail: "stereo:sbs:device:0 or stereo:sbs:https://…", Ready: true, Brand: "XR", Kind: "stereo",
		},
		IngestSource{
			ID: "stereo:ou:", Scheme: "stereo", Label: "Stereo OU (over-under)",
			Detail: "stereo:ou:<src>", Ready: true, Brand: "XR", Kind: "stereo",
		},
		IngestSource{
			ID: "stereo:eq:", Scheme: "stereo", Label: "Full equirect 360",
			Detail: "stereo:eq:<src> · already lat-long", Ready: true, Brand: "XR", Kind: "equirect",
		},
	)

	return out
}

// ParseXRIngest handles xr: / stereo: / webxr: (and aliases).
func ParseXRIngest(src string) (scheme, ref string, ok bool) {
	src = strings.TrimSpace(src)
	if src == "" {
		return "", "", false
	}
	low := strings.ToLower(src)
	if low == "webxr" || low == "webxr:" {
		return "webxr", "", true
	}
	i := strings.Index(src, ":")
	if i <= 0 {
		return "", "", false
	}
	scheme = strings.ToLower(src[:i])
	ref = strings.TrimSpace(src[i+1:])
	ref = strings.TrimPrefix(ref, "//")
	switch scheme {
	case "xr", "ar", "vr", "mr", "headset", "glasses":
		return "xr", ref, true
	case "stereo", "sbs", "ou":
		if scheme == "sbs" {
			return "stereo", "sbs:" + ref, true
		}
		if scheme == "ou" {
			return "stereo", "ou:" + ref, true
		}
		return "stereo", ref, true
	case "webxr":
		return "webxr", ref, true
	case "quest", "vision", "hololens", "magicleap", "pico", "vive", "varjo",
		"xreal", "viture", "rokid", "spectacles", "glass", "openxr":
		// bare brand: → xr:brand
		return "xr", scheme + tern(ref != "", ":"+ref, ""), true
	default:
		return "", "", false
	}
}

func tern(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// ResolveXR turns xr:/stereo:/webxr: into ResolvedStream (or nested device/url).
func ResolveXR(src string) (*ResolvedStream, error) {
	scheme, ref, ok := ParseXRIngest(src)
	if !ok {
		return nil, fmt.Errorf("not an xr scheme")
	}

	switch scheme {
	case "webxr":
		return &ResolvedStream{
			Input: "webxr:", Via: "ingest-webxr", Title: "WebXR browser",
			Format: "webxr", Video: "", // client opens session
		}, nil

	case "xr":
		brandID := ref
		extra := ""
		if j := strings.Index(ref, ":"); j >= 0 {
			brandID = ref[:j]
			extra = ref[j+1:]
		}
		brandID = strings.ToLower(strings.TrimSpace(brandID))
		if brandID == "" || brandID == "auto" {
			// auto: env cast → first matched device → error
			if u := strings.TrimSpace(os.Getenv("GY_XR_CAST_URL")); u != "" {
				return &ResolvedStream{
					Input: src, Video: u, Via: "ingest-xr-cast", Title: "XR cast", Format: "cast",
				}, nil
			}
			for _, d := range listLocalVideoDevices() {
				if ClassifyXRLabel(d.Label) != nil {
					return ResolveIngest(d.ID)
				}
			}
			return nil, fmt.Errorf("xr:auto — no headset device or GY_XR_CAST_URL")
		}
		// brand match on local devices
		for _, d := range listLocalVideoDevices() {
			if b := ClassifyXRLabel(d.Label); b != nil && b.ID == brandID {
				return ResolveIngest(d.ID)
			}
		}
		// env cast for quest-like
		if u := strings.TrimSpace(os.Getenv("GY_XR_CAST_URL")); u != "" {
			return &ResolvedStream{
				Input: src, Video: u, Via: "ingest-xr-cast",
				Title: "XR · " + brandID, Format: brandID,
			}, nil
		}
		// stereo eye pair
		if L := strings.TrimSpace(os.Getenv("GY_XR_LEFT_URL")); L != "" {
			return &ResolvedStream{
				Input: src, Video: L, Via: "ingest-xr-left",
				Title: "XR L · " + brandID, Format: "stereo-L",
			}, nil
		}
		if extra != "" {
			// xr:quest:srt://… or nested device
			if IsIngestSource(extra) || strings.Contains(extra, "://") {
				if IsIngestSource(extra) {
					return ResolveIngest(extra)
				}
				return &ResolvedStream{
					Input: src, Video: extra, Via: "ingest-xr-url", Title: brandID, Format: brandID,
				}, nil
			}
		}
		return nil, fmt.Errorf("xr:%s — no local device; set GY_XR_CAST_URL or use stereo:sbs:<src>", brandID)

	case "stereo":
		// ref = sbs:SRC | ou:SRC | eq:SRC | SRC (default sbs)
		layout := "sbs"
		inner := ref
		if strings.HasPrefix(strings.ToLower(ref), "sbs:") {
			layout = "sbs"
			inner = ref[4:]
		} else if strings.HasPrefix(strings.ToLower(ref), "ou:") {
			layout = "ou"
			inner = ref[3:]
		} else if strings.HasPrefix(strings.ToLower(ref), "eq:") || strings.HasPrefix(strings.ToLower(ref), "equirect:") {
			layout = "equirect"
			if strings.HasPrefix(strings.ToLower(ref), "equirect:") {
				inner = ref[len("equirect:"):]
			} else {
				inner = ref[3:]
			}
		}
		inner = strings.TrimSpace(inner)
		if inner == "" {
			return nil, fmt.Errorf("stereo:%s requires a source (device:0 or URL)", layout)
		}
		// resolve inner
		var base *ResolvedStream
		var err error
		if IsIngestSource(inner) {
			base, err = ResolveIngest(inner)
		} else if strings.Contains(inner, "://") {
			base = &ResolvedStream{Input: inner, Video: inner, Via: "direct", Title: shortURL(inner)}
		} else {
			base, err = ResolveMedia(inner)
		}
		if err != nil {
			return nil, err
		}
		base.Input = "stereo:" + layout + ":" + inner
		base.Via = base.Via + "+stereo-" + layout
		base.Format = "stereo-" + layout
		base.Title = "Stereo " + layout + " · " + base.Title
		return base, nil
	}

	return nil, fmt.Errorf("xr resolve: %s", scheme)
}
