//go:build linux

package agent

// powercapRoot is the sysfs entry point for RAPL energy counters. Both Intel
// and AMD (Zen, kernel >= 5.8) expose package domains here.
const powercapRoot = "/sys/class/powercap"
