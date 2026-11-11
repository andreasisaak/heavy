package sampler

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
)

// ErrNotReachable is why the VM goes unmeasured while docker does not answer (EC-2); the status shows
// `VM: not measured (docker not reachable)`.
var ErrNotReachable = errors.New("docker not reachable")

// restartAfter is how long the VM waits after a failed start or an end of the helper before it starts one again.
const restartAfter = 10 * time.Second

// VMOptions say how the daemon reaches its helper in the Docker VM (design G6).
type VMOptions struct {
	// Docker is the path of the docker program.
	Docker string
	// LinuxBinary is bin/heavy-linux, the build of heavy the helper image holds.
	LinuxBinary string
	// Image is heavy-helper:<stamp>; Container is heavy-helper-<daemon pid>.
	Image     string
	Container string
	// Timeout bounds every request to the helper and every short docker call.
	Timeout time.Duration
	Now     func() time.Time
	Logger  *slog.Logger
}

// VM measures the Docker VM through heavy --helper in a container; it starts the helper on the first request and
// again after the helper ended.
type VM struct {
	options  VMOptions
	mutex    sync.Mutex
	running  *helperProcess
	failure  error
	failedAt time.Time
	closed   bool
}

// NewVM returns a VM that starts nothing until its first request.
func NewVM(options VMOptions) *VM {
	return &VM{options: options}
}

// Sample is the memory, swap, load and pressure of the VM.
func (v *VM) Sample() (helper.Sample, error) {
	response, err := v.request(helper.Request{Type: "sample"})
	if err != nil {
		return helper.Sample{}, err
	}
	if response.Sample == nil {
		return helper.Sample{}, fmt.Errorf("heavy: the helper answered %+v to a sample", response)
	}
	return *response.Sample, nil
}

// Find lists the VM processes that carry HEAVY_RUN=<tag>.
func (v *VM) Find(tag string) ([]helper.Process, error) {
	response, err := v.request(helper.Request{Type: "find", Tag: tag})
	if err != nil {
		return nil, err
	}
	if response.Found == nil {
		return nil, fmt.Errorf("heavy: the helper answered %+v to a find", response)
	}
	return response.Found.Processes, nil
}

// Containers lists the containers of the VM with the resident memory of their processes.
func (v *VM) Containers() ([]helper.Container, error) {
	response, err := v.request(helper.Request{Type: "containers"})
	if err != nil {
		return nil, err
	}
	if response.Usage == nil {
		return nil, fmt.Errorf("heavy: the helper answered %+v to a containers request", response)
	}
	return response.Usage.Containers, nil
}

// Signal sends the signal (STOP, CONT, TERM or KILL) to the PIDs that still carry HEAVY_RUN=<tag>.
func (v *VM) Signal(tag, signal string, pids []int) (helper.Signaled, error) {
	response, err := v.request(helper.Request{Type: "signal", Tag: tag, Signal: signal, PIDs: pids})
	if err != nil {
		return helper.Signaled{}, err
	}
	if response.Signaled == nil {
		return helper.Signaled{}, fmt.Errorf("heavy: the helper answered %+v to a signal", response)
	}
	return *response.Signaled, nil
}

// Close ends the helper; later requests fail.
func (v *VM) Close() error {
	v.mutex.Lock()
	defer v.mutex.Unlock()
	v.closed = true
	if v.running == nil {
		return nil
	}
	err := v.stop(v.running)
	v.running = nil
	return err
}

func (v *VM) request(request helper.Request) (helper.Response, error) {
	v.mutex.Lock()
	defer v.mutex.Unlock()
	if v.closed {
		return helper.Response{}, errors.New("heavy: the VM sampler is closed")
	}
	if v.running == nil {
		if v.failure != nil && v.options.Now().Sub(v.failedAt) < restartAfter {
			return helper.Response{}, v.failure
		}
		running, err := v.start()
		if err != nil {
			v.failure, v.failedAt = err, v.options.Now()
			v.options.Logger.Warn("starting the helper in the VM failed", "error", err.Error())
			return helper.Response{}, err
		}
		v.failure, v.running = nil, running
	}
	response, err := v.running.ask(request, v.options.Timeout)
	if err != nil {
		v.options.Logger.Warn("the helper in the VM failed", "request", request.Type, "error", err.Error())
		if stopError := v.stop(v.running); stopError != nil {
			v.options.Logger.Warn("stopping the helper in the VM failed", "error", stopError.Error())
		}
		v.running, v.failure, v.failedAt = nil, err, v.options.Now()
		return helper.Response{}, err
	}
	if response.Error != "" {
		return helper.Response{}, errors.New(response.Error)
	}
	return response, nil
}
