import { decimalString } from "@/lib/utils"
import type { SystemInfo } from "@/types"

/** Measurable power of a system (CPU package + discrete GPUs), or undefined if the agent reports neither */
export function systemPower(info: Pick<SystemInfo, "cpw" | "gpw">): number | undefined {
	if (info.cpw === undefined && info.gpw === undefined) {
		return undefined
	}
	return (info.cpw ?? 0) + (info.gpw ?? 0)
}

/** Format watts for display, switching to kW at 1000 W */
export function formatWatts(watts: number): string {
	if (watts >= 1000) {
		return `${decimalString(watts / 1000, 2)} kW`
	}
	return `${decimalString(watts, watts >= 100 ? 0 : 1)} W`
}
