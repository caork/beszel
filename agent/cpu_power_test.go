package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeRaplZone(t *testing.T, root, dir, name, energy, maxRange string) {
	t.Helper()
	zoneDir := filepath.Join(root, dir)
	require.NoError(t, os.MkdirAll(zoneDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(zoneDir, "name"), []byte(name+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(zoneDir, "energy_uj"), []byte(energy+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(zoneDir, "max_energy_range_uj"), []byte(maxRange+"\n"), 0o644))
}

func TestDiscoverRaplZones(t *testing.T) {
	root := t.TempDir()
	writeRaplZone(t, root, "intel-rapl:0", "package-0", "1000", "262143328850")
	writeRaplZone(t, root, "intel-rapl:1", "package-1", "2000", "262143328850")
	writeRaplZone(t, root, "intel-rapl:0:0", "core", "500", "262143328850")
	writeRaplZone(t, root, "intel-rapl:2", "psys", "9000", "262143328850")
	writeRaplZone(t, root, "intel-rapl-mmio:0", "package-0", "100", "262143328850")

	zones := discoverRaplZones(root)
	require.Len(t, zones, 2)
	assert.Equal(t, filepath.Join(root, "intel-rapl:0", "energy_uj"), zones[0].energyPath)
	assert.Equal(t, uint64(262143328850), zones[0].maxRangeUj)
	assert.Equal(t, filepath.Join(root, "intel-rapl:1", "energy_uj"), zones[1].energyPath)

	assert.Nil(t, discoverRaplZones(""))
	assert.Nil(t, discoverRaplZones(filepath.Join(root, "missing")))
}

func TestReadCpuPower(t *testing.T) {
	root := t.TempDir()
	writeRaplZone(t, root, "intel-rapl:0", "package-0", "1000000", "10000000")
	writeRaplZone(t, root, "intel-rapl:1", "package-1", "9000000", "10000000")
	zones := discoverRaplZones(root)
	require.Len(t, zones, 2)

	a := &Agent{}
	start := time.Unix(1000, 0)

	_, ok := a.readCpuPower(60000, zones, start)
	assert.False(t, ok, "first reading only sets the baseline")

	// package-0: +30 J; package-1 wraps: 10 J - 9 J + 2 J = 3 J. 33 J over 2 s = 16.5 W
	require.NoError(t, os.WriteFile(zones[0].energyPath, []byte("31000000"), 0o644))
	require.NoError(t, os.WriteFile(zones[1].energyPath, []byte("2000000"), 0o644))
	watts, ok := a.readCpuPower(60000, zones, start.Add(2*time.Second))
	require.True(t, ok)
	assert.InDelta(t, 16.5, watts, 0.0001)

	// a different cache interval keeps its own baseline
	_, ok = a.readCpuPower(1000, zones, start.Add(3*time.Second))
	assert.False(t, ok)

	_, ok = a.readCpuPower(60000, nil, start.Add(4*time.Second))
	assert.False(t, ok)
}

func TestDiscreteGpuPower(t *testing.T) {
	gpus := map[string]system.GPUData{
		"0":     {Name: "RTX PRO 6000", MemoryTotal: 97887, Power: 300},
		"card2": {Name: "AMD iGPU", MemoryTotal: 512, Power: 40},
		"card0": {Name: "Intel iGPU", Power: 5},
	}
	assert.Equal(t, 300.0, discreteGpuPower(gpus, true), "integrated GPUs are part of the CPU package")
	assert.Equal(t, 345.0, discreteGpuPower(gpus, false), "without RAPL every GPU reading counts")
	assert.Equal(t, 0.0, discreteGpuPower(nil, true))
}

func TestRaplZoneCacheRootOverride(t *testing.T) {
	root := t.TempDir()
	writeRaplZone(t, root, "intel-rapl:0", "package-0", "1000", "262143328850")
	t.Setenv("POWERCAP_ROOT", root)
	zones := newRaplZoneCache("/nonexistent")()
	require.Len(t, zones, 1)
	assert.Equal(t, filepath.Join(root, "intel-rapl:0", "energy_uj"), zones[0].energyPath)
}
