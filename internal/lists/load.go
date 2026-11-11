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

// decodeLoad overrides the thresholds a "load" section names; every key is optional.
func decodeLoad(raw json.RawMessage) (Load, bool) {
	fields, isObject := decodeObject(raw)
	load := DefaultLoad()
	if !isObject {
		return Load{}, false
	}
	integers := map[string]*int{"max_parallel": &load.MaxParallel, "host_pressure_percent": &load.HostPressurePercent, "host_recovered_percent": &load.HostRecoveredPercent}
	sizes := map[string]*int64{"reserve_bytes": &load.ReserveBytes, "vm_pressure_bytes": &load.VMPressureBytes, "vm_recovered_bytes": &load.VMRecoveredBytes, "swap_bytes_per_2s": &load.SwapBytesPer2s}
	for key, value := range fields {
		if isNull(value) {
			return Load{}, false
		}
		var err error
		switch {
		case integers[key] != nil:
			err = json.Unmarshal(value, integers[key])
		case sizes[key] != nil:
			err = json.Unmarshal(value, sizes[key])
		case key == "load_per_core":
			err = json.Unmarshal(value, &load.LoadPerCore)
		default:
			return Load{}, false
		}
		if err != nil {
			return Load{}, false
		}
		load.MaxParallelSet = load.MaxParallelSet || key == "max_parallel"
	}
	return load, load.valid()
}

func (l Load) valid() bool {
	percent := func(value int) bool { return value >= 0 && value <= 100 }
	return l.MaxParallel >= 1 && l.ReserveBytes >= 0 && l.VMPressureBytes >= 0 && l.VMRecoveredBytes >= 0 &&
		l.SwapBytesPer2s >= 0 && l.LoadPerCore >= 0 && percent(l.HostPressurePercent) && percent(l.HostRecoveredPercent)
}
