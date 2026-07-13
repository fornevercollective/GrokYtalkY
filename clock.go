package main

import (
	"fmt"
	"sync"
	"time"
)

// Process epoch for wall↔mono drift (epoch drift) measurement.
// Captured once at first sample so Δ tracks local clock wander since start.
var (
	clockEpochOnce sync.Once
	clockEpochWall int64 // UnixNano at start
	clockEpochMono time.Time
	// cached PTP offset refresh
	ptpCacheMu     sync.Mutex
	ptpCacheAt     time.Time
	ptpCacheOffset int64
	ptpCacheMode   PTPMode
)

func ensureClockEpoch() {
	clockEpochOnce.Do(func() {
		now := time.Now()
		clockEpochWall = now.UnixNano()
		clockEpochMono = now
	})
}

// ResetClockEpoch restarts drift measurement (tests / long sessions).
func ResetClockEpoch() {
	now := time.Now()
	clockEpochWall = now.UnixNano()
	clockEpochMono = now
}

// UnixTimeNow returns seconds since Unix epoch with fractional precision.
func UnixTimeNow() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// EpochDriftNs is wall-clock advance minus monotonic advance since process epoch.
// Positive → wall clock running fast vs mono; negative → wall slow / stepped.
// This is the software “epoch drift” indicator when no PTP GM is attached.
func EpochDriftNs() int64 {
	ensureClockEpoch()
	now := time.Now()
	wallDelta := now.UnixNano() - clockEpochWall
	monoDelta := now.Sub(clockEpochMono).Nanoseconds()
	return wallDelta - monoDelta
}

// EpochDriftMs is EpochDriftNs in milliseconds.
func EpochDriftMs() float64 {
	return float64(EpochDriftNs()) / 1e6
}

// PTPOffsetNsCached returns GY_PTP_OFFSET_NS (refreshed every 2s).
func PTPOffsetNsCached() (offset int64, mode PTPMode) {
	ptpCacheMu.Lock()
	defer ptpCacheMu.Unlock()
	if time.Since(ptpCacheAt) < 2*time.Second && ptpCacheAt.Unix() > 0 {
		return ptpCacheOffset, ptpCacheMode
	}
	r := SyncClockFromEnv()
	ptpCacheOffset = r.PTP.OffsetNs
	ptpCacheMode = r.PTP.Mode
	ptpCacheAt = time.Now()
	return ptpCacheOffset, ptpCacheMode
}

// FormatUnixClockLine is a compact top-chrome stamp:
//
//	unix 1712345678.456  Δ+0.82ms  ptp free-run
//
// Δ = local epoch drift (wall−mono). When PTP offset is set, also shows ptpΔ.
func FormatUnixClockLine() string {
	unix := UnixTimeNow()
	driftMs := EpochDriftMs()
	ptpOff, ptpMode := PTPOffsetNsCached()

	// unix with ms
	sec := int64(unix)
	ms := int64((unix - float64(sec)) * 1000)
	if ms < 0 {
		ms = -ms
	}
	driftStr := formatSignedMs(driftMs)
	line := fmt.Sprintf("unix %d.%03d  Δ%s", sec, ms, driftStr)
	if ptpMode == PTPLocked || ptpMode == PTPSlave || ptpOff != 0 {
		line += fmt.Sprintf("  ptpΔ%sns", formatSignedNs(ptpOff))
	}
	line += "  " + string(ptpMode)
	return line
}

// FormatUnixClockCompact for tight header cells (~28–36 cols).
//
//	1712345678.456 Δ+0.8ms
func FormatUnixClockCompact() string {
	unix := UnixTimeNow()
	sec := int64(unix)
	ms := int64((unix - float64(sec)) * 1000)
	if ms < 0 {
		ms = -ms
	}
	return fmt.Sprintf("%d.%03d Δ%s", sec, ms, formatSignedMs(EpochDriftMs()))
}

func formatSignedMs(ms float64) string {
	// show one decimal for sub-ms noise, clamp huge steps
	if ms > 99999 {
		ms = 99999
	}
	if ms < -99999 {
		ms = -99999
	}
	if ms >= 0 {
		return fmt.Sprintf("+%.1fms", ms)
	}
	return fmt.Sprintf("%.1fms", ms) // already has minus
}

func formatSignedNs(ns int64) string {
	if ns >= 0 {
		return fmt.Sprintf("+%d", ns)
	}
	return fmt.Sprintf("%d", ns)
}
