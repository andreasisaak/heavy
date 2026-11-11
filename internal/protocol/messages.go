package protocol

import (
	"bufio"
	"encoding/json"
	"fmt"
)

// The types of messages after the hello.
const (
	TypeRegister  = "register"  // a client announces its run, or announces it again to a new daemon
	TypeWaiting   = "waiting"   // the daemon tells a client its place in the queue
	TypeStart     = "start"     // the daemon lets a client start its command
	TypePause     = "pause"     // pause a run until it is resumed
	TypeResume    = "resume"    // resume a paused run
	TypeTerminate = "terminate" // end a run
	TypeRestart   = "restart"   // the daemon ends a client's command to start it again later; the run waits again
	TypeRed       = "red"       // a client saw a red line of its command; the daemon ends the run (design G8)
	TypePrint     = "print"     // a line the client prints into the output of its run
	TypeEnded     = "ended"     // a client reports the end of its command
	TypeStatus    = "status"    // ask for the queue, and the queue as the answer
	TypeForward   = "forward"   // move a waiting run one place ahead
	TypeSubscribe = "subscribe" // ask for the events of the daemon
	TypeEvent     = "event"     // one event for a subscriber
	TypeError     = "error"     // the other side refused the last message
)

// Message is one line after the hello: its type and the payload of that type.
type Message struct {
	Type      string     `json:"type"`
	Register  *Register  `json:"register,omitempty"`
	Waiting   *Waiting   `json:"waiting,omitempty"`
	Start     *Start     `json:"start,omitempty"`
	Pause     *Target    `json:"pause,omitempty"`
	Resume    *Target    `json:"resume,omitempty"`
	Terminate *Target    `json:"terminate,omitempty"`
	Restart   *Target    `json:"restart,omitempty"`
	Red       *Red       `json:"red,omitempty"`
	Print     *Print     `json:"print,omitempty"`
	Ended     *Ended     `json:"ended,omitempty"`
	Status    *Status    `json:"status,omitempty"`
	Forward   *Forward   `json:"forward,omitempty"`
	Subscribe *Subscribe `json:"subscribe,omitempty"`
	Event     *Event     `json:"event,omitempty"`
	Error     *Failure   `json:"error,omitempty"`
}

// Register announces a run: what it runs, where, for whom. A client that lost its daemon announces its run again
// with Adopt, so the new daemon keeps its id, state and place.
type Register struct {
	Label     string    `json:"label"`
	Command   []string  `json:"command"`
	Directory string    `json:"directory"`
	Checkout  string    `json:"checkout"`
	Worktree  string    `json:"worktree"`
	Session   string    `json:"session"`
	Agent     string    `json:"agent"`
	Pane      string    `json:"pane"`
	ClientPID int       `json:"client_pid"`
	Exclusive bool      `json:"exclusive"`
	Restart   bool      `json:"restart"`
	Adopt     *Adoption `json:"adopt,omitempty"`
}

// Adoption is what a client knows of its run when it announces it to a new daemon.
type Adoption struct {
	ID string `json:"id"`
	// State is "waiting" or "running".
	State string `json:"state"`
	// Since is the arrival of a waiting run or the start of a running one, in Unix seconds.
	Since        int64   `json:"since"`
	Order        float64 `json:"order"`
	ProcessGroup int     `json:"process_group"`
	Cores        int     `json:"cores"`
}

// Waiting is the id, the order and the place of a waiting run.
type Waiting struct {
	ID       string  `json:"id"`
	Order    float64 `json:"order"`
	Position int     `json:"position"`
	Since    int64   `json:"since"`
}

// Start lets a run start with the cores it may use; the client answers with the process group of its command.
type Start struct {
	ID           string `json:"id"`
	Cores        int    `json:"cores"`
	Since        int64  `json:"since"`
	ProcessGroup int    `json:"process_group"`
}

// Red is the line of the command that matched a red pattern of its list entry.
type Red struct {
	Line string `json:"line"`
}

// Target names the run a pause, resume or terminate is for.
type Target struct {
	ID string `json:"id"`
}

// Print is a line for the output of a run.
type Print struct {
	Line string `json:"line"`
}

// Ended is the exit status of a run's command.
type Ended struct {
	ExitStatus int `json:"exit_status"`
	// Red is the red line the client saw, should the command have ended before the daemon heard of it.
	Red string `json:"red,omitempty"`
}

// Status asks for the queue; the answer holds it.
type Status struct {
	Runs []RunStatus `json:"runs"`
	// VM is "" while the daemon measures the Docker VM, else why not, such as `VM: not measured (docker not reachable)`.
	VM string `json:"vm"`
	// Monitors are the sessions with a connected monitor (FR-29).
	Monitors []string `json:"monitors"`
	// Detail asks for Machine as well; the dashboard sets it (design G13).
	Detail bool `json:"detail,omitempty"`
	// Machine is the state of the machine for the dashboard, in answers to a request with Detail; nil before the
	// daemon's first sample.
	Machine *Machine `json:"machine,omitempty"`
}

// Machine is what the dashboard shows of the machine (design G13).
type Machine struct {
	Cores int `json:"cores"`
	// VMMeasured is false while the daemon cannot reach the Docker VM; the host's memory stands in then.
	VMMeasured        bool    `json:"vm_measured"`
	VMAvailableBytes  int64   `json:"vm_available_bytes"`
	SwapOutBytesPer2s int64   `json:"swap_out_bytes_per_2s"`
	Load1             float64 `json:"load1"`
	HostFreePercent   int     `json:"host_free_percent"`
	// FreeSeries is the lowest free memory of the VM in each 3 minutes of the last 30, oldest first; fewer values
	// while the daemon runs shorter, none while the VM is not measured.
	FreeSeries []int64 `json:"free_series"`
	// Foreign are the biggest containers outside the runs, biggest first, from the last look at most 20 s ago.
	Foreign []ForeignContainer `json:"foreign"`
	// Recent are the last results of the history, newest first, at most five.
	Recent []Result `json:"recent"`
}

// ForeignContainer is a container outside the runs with its main checkout and resident memory.
type ForeignContainer struct {
	Name     string `json:"name"`
	Checkout string `json:"checkout"`
	Bytes    int64  `json:"bytes"`
}

// Result is one ended run of the history.
type Result struct {
	Label      string `json:"label"`
	Checkout   string `json:"checkout"`
	ExitStatus int    `json:"exit_status"`
	Reason     string `json:"reason"`
	Started    int64  `json:"started"`
	Ended      int64  `json:"ended"`
}

// RunStatus is one run as `heavy --status --json` prints it (design G14).
type RunStatus struct {
	State          string  `json:"state"`
	PID            int     `json:"pid"`
	Since          int64   `json:"since"`
	Order          float64 `json:"order"`
	Directory      string  `json:"directory"`
	Label          string  `json:"label"`
	Command        string  `json:"command"`
	Agent          string  `json:"agent"`
	Pane           string  `json:"pane"`
	Session        string  `json:"session"`
	Position       int     `json:"position"`
	Ended          int64   `json:"ended"`
	ExitStatus     int     `json:"exit_status"`
	Expires        int64   `json:"expires"`
	ID             string  `json:"id"`
	Checkout       string  `json:"checkout"`
	Budget         int     `json:"budget"`
	RSSBytes       int64   `json:"rss_bytes"`
	PredictedBytes int64   `json:"predicted_bytes"`
	PausedReason   string  `json:"paused_reason"`
	Restarts       int     `json:"restarts"`
	// Waits says what keeps a waiting run from starting now, "" for the other runs. The daemon sends it to a heavy
	// since WaitsSince only: older ones read the status strictly and fail on a field they do not know.
	Waits string `json:"waits,omitempty"`
}

// WaitsSince is the version RunStatus.Waits came with.
const WaitsSince = "1.3.0"

// Forward names the place of the waiting run to move ahead.
type Forward struct {
	Position int `json:"position"`
}

// Subscribe asks for the events of the daemon on behalf of a session's monitor (design G9). The monitor names
// either the PID of its Claude Code process, whose registration $STATE/sessions/<pid>.json names the current
// session also after /clear, or the session itself (`heavy --monitor --session <id>`). The daemon answers with a
// subscribe message once the subscription holds, then sends events until the connection ends.
type Subscribe struct {
	ClaudePID int    `json:"claude_pid,omitempty"`
	Session   string `json:"session,omitempty"`
}

// Event is one notice for the monitors. A notice about a run names the run's session; a notice about a container
// outside the runs names the main checkout of its compose project instead (design G10).
type Event struct {
	Session  string `json:"session,omitempty"`
	Checkout string `json:"checkout,omitempty"`
	Line     string `json:"line"`
}

// Failure is the reason the other side refused a message.
type Failure struct {
	Text string `json:"text"`
}

// payloads names the payloads a message holds.
func (m Message) payloads() []string {
	present := map[string]bool{
		TypeRegister: m.Register != nil, TypeWaiting: m.Waiting != nil, TypeStart: m.Start != nil,
		TypePause: m.Pause != nil, TypeResume: m.Resume != nil, TypeTerminate: m.Terminate != nil, TypeRestart: m.Restart != nil, TypeRed: m.Red != nil,
		TypePrint: m.Print != nil, TypeEnded: m.Ended != nil, TypeStatus: m.Status != nil,
		TypeForward: m.Forward != nil, TypeSubscribe: m.Subscribe != nil, TypeEvent: m.Event != nil,
		TypeError: m.Error != nil,
	}
	var names []string
	for name, held := range present {
		if held {
			names = append(names, name)
		}
	}
	return names
}

// ReadMessage reads one message; it must hold exactly the payload of its type. A field this heavy does not know
// is left out: a newer heavy of the same protocol may send it, and the hooks and shims of an older version that
// runs beside it must not fail on it. A connection closed between
// messages gives io.EOF.
func ReadMessage(reader *bufio.Reader) (Message, error) {
	line, err := readLine(reader)
	if err != nil {
		return Message{}, err
	}
	var message Message
	if err := json.Unmarshal(line, &message); err != nil {
		return Message{}, fmt.Errorf("heavy: expected a message of protocol %d, got %s: %w", Version, line, err)
	}
	if payloads := message.payloads(); len(payloads) != 1 || payloads[0] != message.Type {
		return Message{}, fmt.Errorf("heavy: expected a message whose payload fits its type, got %s", line)
	}
	return message, nil
}
