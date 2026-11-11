package procs

// RSS sums the resident memory of the processes of the group from /proc; a group without processes has none.
func (SystemHost) RSS(group int) (int64, error) {
	return GroupRSS("/proc", group)
}
