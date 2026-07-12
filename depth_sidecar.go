package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ZipDepth HTTP sidecar protocol (aito-mac zipdepth-sidecar):
//   GET  /health → { ok, backend, zipdepth }
//   POST /depth  → body: u32le w, u32le h, RGB888…  response JSON { w,h,depth:[], backend }

const defaultZipDepthURL = "http://127.0.0.1:8766"

func zipDepthBaseURL() string {
	if u := os.Getenv("ZIPDEPTH_URL"); u != "" {
		return u
	}
	if u := os.Getenv("GY_ZIPDEPTH"); u != "" {
		return u
	}
	return defaultZipDepthURL
}

// ZipDepthHealth probes the sidecar.
func ZipDepthHealth() (backend string, realZip bool, err error) {
	client := &http.Client{Timeout: 800 * time.Millisecond}
	res, err := client.Get(zipDepthBaseURL() + "/health")
	if err != nil {
		return "", false, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", false, fmt.Errorf("HTTP %s", res.Status)
	}
	var body struct {
		OK       bool   `json:"ok"`
		Backend  string `json:"backend"`
		ZipDepth bool   `json:"zipdepth"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return "", false, err
	}
	return body.Backend, body.ZipDepth, nil
}

// FetchZipDepth sends RGB frame to sidecar; returns normalized depth map.
func FetchZipDepth(rgb []byte, w, h int) (*DepthMap, error) {
	if w < 1 || h < 1 || len(rgb) < w*h*3 {
		return nil, fmt.Errorf("bad frame")
	}
	// Downscale large frames for latency (sidecar often 256–384)
	sw, sh := w, h
	maxSide := 256
	if w > maxSide || h > maxSide {
		if w >= h {
			sw = maxSide
			sh = max(2, h*maxSide/w)
		} else {
			sh = maxSide
			sw = max(2, w*maxSide/h)
		}
		rgb = downsampleRGB(rgb, w, h, sw, sh)
		w, h = sw, sh
	}

	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint32(w))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(h))
	buf.Write(rgb[:w*h*3])

	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Post(zipDepthBaseURL()+"/depth", "application/octet-stream", &buf)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 200))
		return nil, fmt.Errorf("depth %s: %s", res.Status, string(b))
	}
	var body struct {
		W       int       `json:"w"`
		H       int       `json:"h"`
		Depth   []float64 `json:"depth"`
		Backend string    `json:"backend"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}
	if len(body.Depth) == 0 {
		return nil, fmt.Errorf("empty depth")
	}
	bw, bh := body.W, body.H
	if bw < 1 || bh < 1 {
		// infer square-ish
		n := len(body.Depth)
		bw = w
		bh = h
		if bw*bh != n {
			// try reshape
			bw = int(mathSqrt(n))
			bh = n / max(1, bw)
		}
	}
	via := "zipdepth"
	if body.Backend != "" {
		via = body.Backend
	}
	// if sidecar size differs from request, still accept
	if bw*bh != len(body.Depth) {
		// pad/truncate
		z := make([]float64, w*h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				sx := x * bw / w
				sy := y * bh / h
				if sx >= bw {
					sx = bw - 1
				}
				if sy >= bh {
					sy = bh - 1
				}
				i := sy*bw + sx
				if i >= 0 && i < len(body.Depth) {
					z[y*w+x] = body.Depth[i]
				}
			}
		}
		return &DepthMap{W: w, H: h, Z: z, Via: via, Stamp: time.Now()}, nil
	}
	return &DepthMap{W: bw, H: bh, Z: body.Depth, Via: via, Stamp: time.Now()}, nil
}

func mathSqrt(n int) int {
	if n <= 0 {
		return 1
	}
	x := n
	for {
		y := (x + n/x) / 2
		if y >= x {
			return x
		}
		x = y
	}
}

func downsampleRGB(rgb []byte, w, h, nw, nh int) []byte {
	out := make([]byte, nw*nh*3)
	for y := 0; y < nh; y++ {
		sy := y * h / nh
		for x := 0; x < nw; x++ {
			sx := x * w / nw
			si := (sy*w + sx) * 3
			di := (y*nw + x) * 3
			out[di] = rgb[si]
			out[di+1] = rgb[si+1]
			out[di+2] = rgb[si+2]
		}
	}
	return out
}

// DepthDoctorLine for gy doctor.
func DepthDoctorLine() string {
	be, real, err := ZipDepthHealth()
	if err != nil {
		return fmt.Sprintf("  · zipdepth sidecar  down (%s) — zip-lite/gsplat still work\n    start: scripts/zipdepth-sidecar.sh\n    or:    python3 ~/dev/aito-mac/zipdepth-sidecar/booth_zipdepth.py", defaultZipDepthURL)
	}
	tag := "zip-lite backend"
	if real {
		tag = "real ZipDepth model"
	}
	return fmt.Sprintf("  ✓ zipdepth sidecar  %s · %s", be, tag)
}
