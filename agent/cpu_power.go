package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/henrygd/beszel/agent/utils"
	"github.com/henrygd/beszel/internal/entities/system"
)

// integratedGpuMaxVramMB is the VRAM size below which a GPU is treated as
// integrated. Integrated GPUs (Intel iGPU, AMD APU / Zen 4+ iGPU) draw from the
// CPU package, so their power is already part of the RAPL package reading and
// must not be added to the total a second time.
const integratedGpuMaxVramMB = 4096

// raplZone is one top-level RAPL package domain (e.g. intel-rapl:0 "package-0").
type raplZone struct {
	energyPath string
	maxRangeUj uint64
}

type raplSample struct {
	energyUj []uint64
	at       time.Time
}

var getRaplZones = newRaplZoneCache(powercapRoot)

// newRaplZoneCache discovers zones once. POWERCAP_ROOT overrides the default
// root: Docker masks /sys/devices/virtual/powercap, so containers need it
// bind-mounted elsewhere, e.g.
// -v /sys/devices/virtual/powercap/intel-rapl:/host/powercap:ro with
// POWERCAP_ROOT=/host/powercap.
func newRaplZoneCache(root string) func() []raplZone {
	return sync.OnceValue(func() []raplZone {
		if override, ok := utils.GetEnv("POWERCAP_ROOT"); ok {
			root = override
		}
		zones := discoverRaplZones(root)
		if len(zones) > 0 {
			slog.Debug("RAPL package zones", "count", len(zones))
		}
		return zones
	})
}

// discoverRaplZones returns the top-level package domains under the powercap
// root. Sub-zones (core/uncore/dram, "intel-rapl:0:0") are skipped because they
// are already included in their package, and "psys" is skipped because it is a
// platform domain that overlaps the packages.
func discoverRaplZones(root string) []raplZone {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var zones []raplZone
	for _, entry := range entries {
		name := entry.Name()
		// top-level zones have exactly one colon: intel-rapl:0
		if !strings.HasPrefix(name, "intel-rapl:") || strings.Count(name, ":") != 1 {
			continue
		}
		dir := filepath.Join(root, name)
		if !strings.HasPrefix(utils.ReadStringFile(filepath.Join(dir, "name")), "package") {
			continue
		}
		energyPath := filepath.Join(dir, "energy_uj")
		// energy_uj is root-only on kernels >= 5.10; skip zones we can't read
		if _, ok := utils.ReadUintFile(energyPath); !ok {
			continue
		}
		maxRange, _ := utils.ReadUintFile(filepath.Join(dir, "max_energy_range_uj"))
		zones = append(zones, raplZone{energyPath: energyPath, maxRangeUj: maxRange})
	}
	return zones
}

// readCpuPower returns the average CPU package power in watts since the last
// call with the same cacheTimeMs. The first call per interval only records a
// baseline and returns ok=false.
func (a *Agent) readCpuPower(cacheTimeMs uint16, zones []raplZone, now time.Time) (watts float64, ok bool) {
	if len(zones) == 0 {
		return 0, false
	}
	current := raplSample{energyUj: make([]uint64, len(zones)), at: now}
	for i, zone := range zones {
		v, ok := utils.ReadUintFile(zone.energyPath)
		if !ok {
			return 0, false
		}
		current.energyUj[i] = v
	}
	if a.raplSamples == nil {
		a.raplSamples = make(map[uint16]raplSample)
	}
	prev, hasPrev := a.raplSamples[cacheTimeMs]
	a.raplSamples[cacheTimeMs] = current
	if !hasPrev || len(prev.energyUj) != len(current.energyUj) {
		return 0, false
	}
	seconds := current.at.Sub(prev.at).Seconds()
	if seconds <= 0 {
		return 0, false
	}
	var totalUj uint64
	for i, zone := range zones {
		delta := current.energyUj[i] - prev.energyUj[i]
		if current.energyUj[i] < prev.energyUj[i] {
			// counter wrapped
			if zone.maxRangeUj == 0 {
				return 0, false
			}
			delta = zone.maxRangeUj - prev.energyUj[i] + current.energyUj[i]
		}
		totalUj += delta
	}
	return float64(totalUj) / 1e6 / seconds, true
}

// updatePower fills Stats.CpuPower and the dashboard power fields in Info.
// Must run after GPU data has been collected.
func (a *Agent) updatePower(cacheTimeMs uint16, systemStats *system.Stats) {
	cpuWatts, hasCpu := a.readCpuPower(cacheTimeMs, getRaplZones(), time.Now())
	if hasCpu {
		systemStats.CpuPower = utils.TwoDecimals(cpuWatts)
	}
	gpuWatts := discreteGpuPower(systemStats.GPUData, hasCpu)

	// Real-time (1s) requests must not overwrite the dashboard values with a
	// missing baseline; keep the last value until a reading is available.
	if hasCpu || cacheTimeMs == defaultDataCacheTimeMs {
		a.systemInfo.CpuPower = systemStats.CpuPower
	}
	a.systemInfo.GpuPower = utils.TwoDecimals(gpuWatts)
}

// discreteGpuPower sums GPU board power. When the CPU package reading is
// available, integrated GPUs are excluded because RAPL already covers them.
func discreteGpuPower(gpus map[string]system.GPUData, hasCpuPower bool) float64 {
	var total float64
	for _, gpu := range gpus {
		if hasCpuPower && gpu.MemoryTotal < integratedGpuMaxVramMB {
			continue
		}
		total += gpu.Power
	}
	return total
}
