package monitor

// ReadProcess reads a process from /proc.
func ReadProcess(pid int) (Process, error) {
	return ReadProcProcess("/proc", pid)
}
