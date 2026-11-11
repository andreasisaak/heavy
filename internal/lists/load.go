package lists

import "encoding/json"

// Load holds the thresholds of the queue (design G4 and G5), from the "load" section of the personal list.
type Load struct {
	// MaxParallel is the most runs active at once, running or paused.
	MaxParallel int
	// MaxParallelSet is true when the personal list names max_parallel; without it, the limit of a machine
	// configuration stands in for the default.
	MaxParallelSet bool
	// ReserveBytes must stay free in the VM after the expected peak of a run that starts.
	ReserveBytes int64
	// VMPressureBytes: less available memory in the VM for 10 s is pressure.
	VMPressureBytes int64
	// VMRecoveredBytes: at least this much for 30 s counts as recovered.
	VMRecoveredBytes int64
	// HostPressurePercent: a lower kern.memorystatus_level for 10 s is pressure.
	HostPressurePercent int
	// HostRecoveredPercent: at least this level admits runs and, for 30 s, counts as recovered.
	HostRecoveredPercent int
	// LoadPerCore: a higher load average per core admits no run.
	LoadPerCore float64
	// SwapBytesPer2s: more swapped out per 2 s, averaged over 30 s, admits no run.
	SwapBytesPer2s int64
	// UnpredictedPeakBytes is the peak memory to expect of a run without history, with which it may start beside
	// other runs; 0, the default, lets such a run start only alone.
	UnpredictedPeakBytes int64
}

// DefaultLoad returns the thresholds of design G4 and G5.
func DefaultLoad() Load {
	return Load{
		MaxParallel:          3,
		ReserveBytes:         2 << 30,
		VMPressureBytes:      3 << 29,
		VMRecoveredBytes:     5 << 29,
		HostPressurePercent:  10,
		HostRecoveredPercent: 20,
		LoadPerCore:          0.8,
		SwapBytesPer2s:       4 << 20,
	}
}

// decodeLoad overrides the thresholds a "load" section names; every key is optional. A key heavy does not know and
// a value it cannot use are noted, and the threshold keeps its default.
func decodeLoad(raw json.RawMessage, n *notes) Load {
	load := DefaultLoad()
	fields, isObject := decodeObject(raw)
	if !isObject {
		n.unusable("load", raw, "an object", "heavy uses the default thresholds")
		return load
	}
	limits := map[string]*int{"max_parallel": &load.MaxParallel}
	percents := map[string]*int{"host_pressure_percent": &load.HostPressurePercent, "host_recovered_percent": &load.HostRecoveredPercent}
	sizes := map[string]*int64{"reserve_bytes": &load.ReserveBytes, "vm_pressure_bytes": &load.VMPressureBytes, "vm_recovered_bytes": &load.VMRecoveredBytes,
		"swap_bytes_per_2s": &load.SwapBytesPer2s, "unpredicted_peak_bytes": &load.UnpredictedPeakBytes}
	for _, key := range sortedKeys(fields) {
		value := fields[key]
		var whole int64
		var number float64
		isWhole := !isNull(value) && json.Unmarshal(value, &whole) == nil
		switch {
		case limits[key] != nil && isWhole && whole >= 1:
			*limits[key], load.MaxParallelSet = int(whole), true
		case limits[key] != nil:
			n.unusable("load."+key, value, "a whole number from 1", "heavy uses its default")
		case percents[key] != nil && isWhole && whole >= 0 && whole <= 100:
			*percents[key] = int(whole)
		case percents[key] != nil:
			n.unusable("load."+key, value, "a whole number from 0 to 100", "heavy uses its default")
		case sizes[key] != nil && isWhole && whole >= 0:
			*sizes[key] = whole
		case sizes[key] != nil:
			n.unusable("load."+key, value, "whole bytes from 0", "heavy uses its default")
		case key == "load_per_core" && !isNull(value) && json.Unmarshal(value, &number) == nil && number >= 0:
			load.LoadPerCore = number
		case key == "load_per_core":
			n.unusable("load."+key, value, "a number from 0", "heavy uses its default")
		default:
			n.unknown("load." + key)
		}
	}
	return load
}
