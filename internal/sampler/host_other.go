//go:build !darwin && !linux

package sampler

import "errors"

// ReadHost exists for macOS and Linux only.
func ReadHost() (HostSample, error) {
	return HostSample{}, errors.New("heavy: measuring the host exists for macOS and Linux only")
}
