// Package helper is `heavy --helper`, the side of heavy inside the Docker VM: it reads /proc of the VM and answers
// the daemon's requests as JSON lines on stdin and stdout (design G6).
package helper

// Request is one request of the daemon: sample, find (tag), containers, or signal (tag, signal, pids).
type Request struct {
	Type   string `json:"type"`
	Tag    string `json:"tag,omitempty"`
	Signal string `json:"signal,omitempty"`
	PIDs   []int  `json:"pids,omitempty"`
}

// Response carries exactly one of its fields: the answer to the request or the error.
type Response struct {
	Sample   *Sample   `json:"sample,omitempty"`
	Found    *Found    `json:"found,omitempty"`
	Usage    *Usage    `json:"usage,omitempty"`
	Signaled *Signaled `json:"signaled,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// Sample is the memory, swap, load and pressure of the VM.
type Sample struct {
	MemAvailableBytes int64 `json:"mem_available_bytes"`
	// SwapOutBytes counts the bytes swapped out since the VM booted (pswpout times the page size).
	SwapOutBytes int64     `json:"swap_out_bytes"`
	Load1        float64   `json:"load1"`
	Cores        int       `json:"cores"`
	Pressure     *Pressure `json:"pressure,omitempty"`
}

// Pressure is the pressure stall information of the VM, absent when the kernel has none.
type Pressure struct {
	Memory Stall `json:"memory"`
	CPU    Stall `json:"cpu"`
	IO     Stall `json:"io"`
}

// Stall is the share of time in percent, over the last 10 s, in which some or all tasks waited for a resource.
type Stall struct {
	Some float64 `json:"some"`
	Full float64 `json:"full"`
}

// Found lists the processes that carry a tag, by PID.
type Found struct {
	Processes []Process `json:"processes"`
}

// Process is one process of the VM with its resident memory and the id of its container, "" outside containers.
type Process struct {
	PID       int    `json:"pid"`
	RSSBytes  int64  `json:"rss_bytes"`
	Container string `json:"container"`
}

// Usage lists the containers of the VM with the resident memory of their processes, by id.
type Usage struct {
	Containers []Container `json:"containers"`
}

// Container is the resident memory of the processes of one container.
type Container struct {
	ID       string `json:"id"`
	RSSBytes int64  `json:"rss_bytes"`
}

// Signaled names the PIDs a signal reached and those it skipped because they no longer carried the tag.
type Signaled struct {
	Sent    []int `json:"sent"`
	Skipped []int `json:"skipped"`
}
