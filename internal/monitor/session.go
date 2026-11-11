package monitor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sessions"
)

// target is whom the monitor subscribes for: the Claude Code PID it found among its ancestors, or the session of
// --session.
type target struct {
	claudePID int
	session   string
}

// place is where the session stands at the time of an event: its id and its main checkout.
type place struct {
	session  string
	checkout string
}

func (t target) subscription() protocol.Subscribe {
	return protocol.Subscribe{ClaudePID: t.claudePID, Session: t.session}
}

// place reads where the session stands now; after /clear the registration of the Claude Code PID names a new
// session.
func (t target) place(stateDirectory string) (place, error) {
	if t.session != "" {
		checkout, err := checkoutOf(stateDirectory, t.session)
		return place{session: t.session, checkout: checkout}, err
	}
	registration, err := sessions.Read(stateDirectory, t.claudePID)
	if err != nil {
		return place{}, err
	}
	return place{session: registration.SessionID, checkout: registration.Checkout}, nil
}

// wants says whether the event is meant for the session: a notice about a run names its session, a notice about a
// container outside the runs names the main checkout of its compose project.
func (p place) wants(event protocol.Event) bool {
	if event.Session != "" {
		return event.Session == p.session
	}
	return event.Checkout != "" && event.Checkout == p.checkout
}

// checkoutOf is the main checkout of the newest registration of the session, "" when no registration names it.
func checkoutOf(stateDirectory, session string) (string, error) {
	folder := filepath.Join(stateDirectory, "sessions")
	entries, err := os.ReadDir(folder)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("heavy: listing the session registrations in %s: %w", folder, err)
	}
	checkout, newest, found := "", int64(0), false
	for _, entry := range entries {
		name, isRegistration := strings.CutSuffix(entry.Name(), ".json")
		if !isRegistration {
			continue
		}
		pid, err := strconv.Atoi(name)
		if err != nil {
			return "", fmt.Errorf("heavy: expected the session registrations in %s to be named <pid>.json, found %s", folder, entry.Name())
		}
		registration, err := sessions.Read(stateDirectory, pid)
		if err != nil {
			return "", err
		}
		if registration.SessionID == session && (!found || registration.Time > newest) {
			checkout, newest, found = registration.Checkout, registration.Time, true
		}
	}
	return checkout, nil
}
