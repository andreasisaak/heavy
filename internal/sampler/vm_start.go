package sampler

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/andreasisaak/heavy/internal/dockercli"
)

// buildTimeout bounds the build of the helper image, which copies one binary into an empty image.
const buildTimeout = 60 * time.Second

// dockerfile is the image of the helper: heavy-linux alone, started as heavy --helper.
const dockerfile = "FROM scratch\nCOPY heavy-linux /heavy\nENTRYPOINT [\"/heavy\", \"--helper\"]\n"

// start checks docker, builds the image if it is missing and starts the helper container.
func (v *VM) start() (*helperProcess, error) {
	if _, err := v.docker(v.options.Timeout, "", "version", "--format", "{{.Server.Version}}"); err != nil {
		v.options.Logger.Info("docker does not answer", "error", err.Error())
		return nil, ErrNotReachable
	}
	if _, err := os.Stat(v.options.LinuxBinary); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("heavy: %s is missing, so heavy measures no VM", v.options.LinuxBinary)
	} else if err != nil {
		return nil, fmt.Errorf("heavy: reading %s: %w", v.options.LinuxBinary, err)
	}
	if _, err := v.docker(v.options.Timeout, "", "image", "inspect", "--format", "{{.Id}}", v.options.Image); err != nil {
		if err := v.build(); err != nil {
			return nil, err
		}
	}
	if _, err := v.docker(v.options.Timeout, "", "rm", "-f", v.options.Container); err != nil {
		return nil, fmt.Errorf("heavy: removing an old container %s failed: %w", v.options.Container, err)
	}
	command := exec.Command(v.options.Docker, "run", "--rm", "-i", "--pid=host", "--cap-add=SYS_PTRACE", "--cap-add=KILL",
		"--name", v.options.Container, v.options.Image)
	running, err := startHelper(command)
	if err != nil {
		return nil, err
	}
	v.options.Logger.Info("started the helper in the VM", "container", v.options.Container, "image", v.options.Image)
	return running, nil
}

// build makes the helper image offline from heavy-linux in a context folder of its own.
func (v *VM) build() error {
	folder, err := os.MkdirTemp("", "heavy-helper-")
	if err != nil {
		return fmt.Errorf("heavy: making a folder for the helper image: %w", err)
	}
	defer os.RemoveAll(folder)
	content, err := os.ReadFile(v.options.LinuxBinary)
	if err != nil {
		return fmt.Errorf("heavy: reading %s: %w", v.options.LinuxBinary, err)
	}
	if err := os.WriteFile(filepath.Join(folder, "heavy-linux"), content, 0o755); err != nil {
		return fmt.Errorf("heavy: copying %s for the helper image: %w", v.options.LinuxBinary, err)
	}
	if _, err := v.docker(buildTimeout, dockerfile, "build", "-q", "-t", v.options.Image, "-f", "-", folder); err != nil {
		return fmt.Errorf("heavy: building the image %s failed: %w", v.options.Image, err)
	}
	v.options.Logger.Info("built the helper image", "image", v.options.Image)
	return nil
}

// docker runs a short docker call within the timeout.
func (v *VM) docker(timeout time.Duration, input string, arguments ...string) (string, error) {
	return dockercli.Run(v.options.Docker, timeout, input, arguments...)
}

// stop ends a helper through the end of its input; one that stays is removed with its container and killed.
func (v *VM) stop(running *helperProcess) error {
	if running.close(v.options.Timeout) {
		return nil
	}
	_, err := v.docker(v.options.Timeout, "", "rm", "-f", v.options.Container)
	running.kill(v.options.Timeout)
	if err != nil {
		return fmt.Errorf("heavy: removing the container %s failed: %w", v.options.Container, err)
	}
	return nil
}
