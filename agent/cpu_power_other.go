//go:build !linux

package agent

// powercapRoot is empty on non-Linux platforms; RAPL via sysfs is Linux-only.
const powercapRoot = ""
