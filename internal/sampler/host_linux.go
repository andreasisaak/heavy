package sampler

import "os"

// ReadHost measures the host from /proc.
func ReadHost() (HostSample, error) {
	return ReadProcHost("/proc", int64(os.Getpagesize()))
}
