package sessionstart_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/shell"
	"github.com/andreasisaak/heavy/internal/testkit"
)

const sessionLine = "export HEAVY_SESSION=5968af83-2137-45a0-b3d2-6d3a974c6814\n"

// pathLine is the line with which every heavy puts a shims folder in front of PATH, and with which other plugins
// put their own folders there.
func pathLine(folder string) string {
	quoted := shell.Quote(folder)
	return "case \"$PATH\" in " + quoted + ":*) ;; *) export PATH=" + quoted + ":\"$PATH\" ;; esac\n"
}

// retiredLine is pathLine after the hook took it out of use: a comment of the same length.
func retiredLine(folder string) string {
	return "# heavy, stale: " + strings.TrimPrefix(pathLine(folder), "case \"$PATH\" in ")
}

// scriptShims is the shims folder of a heavy 0.x: scripts that run heavy-shim next to them.
func scriptShims(t *testing.T, parent string) string {
	t.Helper()
	shims := filepath.Join(parent, "shims")
	if err := os.MkdirAll(shims, 0o755); err != nil {
		t.Fatal(err)
	}
	testkit.FakeProgram(t, shims, "heavy-shim", "echo old heavy; exit 1")
	testkit.FakeProgram(t, shims, "uv", `exec /bin/bash "${0%/*}/heavy-shim" "${0##*/}" ${1+"$@"}`)
	return shims
}

// linkShims is the shims folder of a heavy 1.x: links to the heavy binary of that version.
func linkShims(t *testing.T, parent string) string {
	t.Helper()
	shims := filepath.Join(parent, "shims")
	for _, folder := range []string{shims, filepath.Join(parent, "bin")} {
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	testkit.FakeProgram(t, filepath.Join(parent, "bin"), "heavy", "exit 0")
	if err := os.Symlink("../bin/heavy", filepath.Join(shims, "make")); err != nil {
		t.Fatal(err)
	}
	return shims
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// start runs the hook with a working directory that is no checkout.
func start(t *testing.T, envFile string) (int, string, string) {
	t.Helper()
	status, stdout, stderr, _ := hook(t, strings.Replace(sessionStart, "%s", t.TempDir(), 1), envFile)
	return status, stdout, stderr
}

// 2026-11-23: claude --resume applies the env files of the earlier session starts again. The hook of heavy 0.6.0
// had the number 4 there, the one of 1.3.0 the number 6, so the file of 0.6.0 stayed, with the line of another
// plugin that has the number 4 now, and put the script shims of 0.6.0 on PATH.
func TestAResumedSessionLosesTheShimsOfHeavyZero(t *testing.T) {
	for _, shellPath := range []string{"/bin/bash", "/bin/zsh"} {
		t.Run(shellPath, func(t *testing.T) {
			if _, err := os.Stat(shellPath); err != nil {
				t.Skipf("this machine has no %s", shellPath)
			}
			session := t.TempDir()
			old := scriptShims(t, t.TempDir())
			other := filepath.Join(t.TempDir(), "plan")
			earlier, envFile := filepath.Join(session, "sessionstart-hook-4.sh"), filepath.Join(session, "sessionstart-hook-6.sh")
			writeFile(t, earlier, pathLine(old)+sessionLine+pathLine(other))

			status, stdout, stderr, deps := hook(t, strings.Replace(sessionStart, "%s", t.TempDir(), 1), envFile)
			command := exec.Command(shellPath, "-c", `. "`+earlier+`"; . "`+envFile+`"; printf "%s" "$PATH"`)
			command.Env = []string{"PATH=/usr/bin:/bin"}
			path, err := command.Output()

			wantNote := "heavy: took the shims of an earlier heavy off PATH: " + old + " in " + earlier + "\n"
			if status != 0 || stdout != "" || stderr != wantNote || err != nil {
				t.Fatalf("hook = %d, %q, %q; sourcing: %v; want 0 and the note %q", status, stdout, stderr, err, wantNote)
			}
			if got, want := readFile(t, earlier), retiredLine(old)+sessionLine+pathLine(other); got != want {
				t.Fatalf("the earlier env file = %q, want %q", got, want)
			}
			if want := deps.StateDirectory + "/current/shims:" + other + ":/usr/bin:/bin"; string(path) != want {
				t.Fatalf("PATH after sourcing = %q, want %q", path, want)
			}
		})
	}
}

// The hook of 1.0.4 and the one of 1.3.0 can have the same number, so the line of 1.0.4 stands in the file the
// hook appends to.
func TestTheOwnEnvFileLosesTheLinkShimsOfAnEarlierHeavy(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "sessionstart-hook-4.sh")
	old := linkShims(t, t.TempDir())
	writeFile(t, envFile, pathLine(old)+sessionLine)

	status, stdout, stderr, deps := hook(t, strings.Replace(sessionStart, "%s", t.TempDir(), 1), envFile)

	wantNote := "heavy: took the shims of an earlier heavy off PATH: " + old + " in " + envFile + "\n"
	if status != 0 || stdout != "" || stderr != wantNote {
		t.Fatalf("hook = %d, %q, %q; want 0 and the note %q", status, stdout, stderr, wantNote)
	}
	want := retiredLine(old) + sessionLine + pathLine(deps.StateDirectory+"/current/shims") + sessionLine
	if got := readFile(t, envFile); got != want {
		t.Fatalf("the env file = %q, want %q", got, want)
	}
}

// Other hooks of the same session start append to their env files while this hook runs. The hook therefore changes
// bytes in place: the file keeps its length and stays the same file, so no line another hook appends is lost.
func TestRetiringALineKeepsTheFileAndItsLength(t *testing.T) {
	session := t.TempDir()
	old := scriptShims(t, t.TempDir())
	earlier := filepath.Join(session, "sessionstart-hook-4.sh")
	writeFile(t, earlier, "export FIRST=1\n"+pathLine(old)+"export LAST=1\n")
	before, err := os.Stat(earlier)
	if err != nil {
		t.Fatal(err)
	}
	appender, err := os.OpenFile(earlier, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer appender.Close()

	status, _, _ := start(t, filepath.Join(session, "sessionstart-hook-6.sh"))
	if _, err := appender.WriteString("export APPENDED=1\n"); err != nil {
		t.Fatal(err)
	}

	after, err := os.Stat(earlier)
	if status != 0 || err != nil || !os.SameFile(before, after) {
		t.Fatalf("hook = %d, stat %v, same file %v; want 0 and the file the other hook has open", status, err, os.SameFile(before, after))
	}
	want := "export FIRST=1\n" + retiredLine(old) + "export LAST=1\nexport APPENDED=1\n"
	if got := readFile(t, earlier); got != want || len(retiredLine(old)) != len(pathLine(old)) {
		t.Fatalf("the earlier env file = %q, want %q with a retired line as long as the line", got, want)
	}
}

// The hook of another plugin opens the file, appends a line and closes it, as often as it likes, while this hook
// retires a line of the same file: every appended line is there afterwards, in its order.
func TestLinesAppendedWhileTheHookRunsAreAllKept(t *testing.T) {
	old := scriptShims(t, t.TempDir())
	for round := 0; round < 20; round++ {
		session := t.TempDir()
		earlier := filepath.Join(session, "sessionstart-hook-4.sh")
		writeFile(t, earlier, pathLine(old))
		appended, failed := "", make(chan error, 1)
		for line := 0; line < 200; line++ {
			appended += fmt.Sprintf("export LINE_%d=1\n", line)
		}
		go func() {
			for _, line := range strings.SplitAfter(appended, "\n") {
				handle, err := os.OpenFile(earlier, os.O_APPEND|os.O_WRONLY, 0o644)
				if err != nil {
					failed <- err
					return
				}
				if _, err := handle.WriteString(line); err != nil {
					failed <- err
					return
				}
				if err := handle.Close(); err != nil {
					failed <- err
					return
				}
			}
			failed <- nil
		}()

		status, _, _ := start(t, filepath.Join(session, "sessionstart-hook-6.sh"))

		if err := <-failed; err != nil || status != 0 {
			t.Fatalf("round %d: hook = %d, appending: %v", round, status, err)
		}
		if got, want := readFile(t, earlier), retiredLine(old)+appended; got != want {
			t.Fatalf("round %d: the earlier env file = %q, want %q", round, got, want)
		}
	}
}

func TestASecondSessionStartChangesNoRetiredLine(t *testing.T) {
	session := t.TempDir()
	old := scriptShims(t, t.TempDir())
	earlier := filepath.Join(session, "sessionstart-hook-4.sh")
	writeFile(t, earlier, pathLine(old))
	start(t, filepath.Join(session, "sessionstart-hook-6.sh"))

	status, stdout, stderr := start(t, filepath.Join(session, "sessionstart-hook-6.sh"))

	if status != 0 || stdout != "" || stderr != "" {
		t.Fatalf("second hook = %d, %q, %q; want 0 and nothing", status, stdout, stderr)
	}
	if got := readFile(t, earlier); got != retiredLine(old) {
		t.Fatalf("the earlier env file = %q, want %q", got, retiredLine(old))
	}
}

// A folder with a space or a quote in its name is quoted in the line.
func TestALineWithAQuotedFolderIsRetired(t *testing.T) {
	session := t.TempDir()
	old := scriptShims(t, filepath.Join(t.TempDir(), "it's an old heavy"))
	earlier := filepath.Join(session, "sessionstart-hook-4.sh")
	writeFile(t, earlier, pathLine(old))

	status, _, stderr := start(t, filepath.Join(session, "sessionstart-hook-6.sh"))

	wantNote := "heavy: took the shims of an earlier heavy off PATH: " + old + " in " + earlier + "\n"
	if got := readFile(t, earlier); status != 0 || stderr != wantNote || got != retiredLine(old) {
		t.Fatalf("hook = %d, %q, file %q; want 0, %q and %q", status, stderr, got, wantNote, retiredLine(old))
	}
}

// Only a line that puts a shims folder of heavy on PATH goes. Everything else in the env files of the session
// stays byte for byte: the lines of other plugins, also in the form heavy uses and for a folder named shims, the
// line for the shims behind the link of this heavy, a folder that is gone, a line that only holds such a line, and
// a line that names the folder in another quoting than heavy writes.
func TestLinesThatPutNoEarlierHeavyOnPathStay(t *testing.T) {
	session := t.TempDir()
	envFile := filepath.Join(session, "sessionstart-hook-6.sh")
	foreign := filepath.Join(t.TempDir(), "shims")
	if err := os.Mkdir(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	testkit.FakeProgram(t, foreign, "uv", "exit 0")
	if err := os.Symlink("/bin/sh", filepath.Join(foreign, "sh")); err != nil {
		t.Fatal(err)
	}
	old := scriptShims(t, t.TempDir())
	gone := filepath.Join(t.TempDir(), "heavy", "0.3.0", "shims")
	inside := strings.TrimSuffix(pathLine(old), "\n")
	// heavy quotes a folder with a space; a line that names one without quotes is not from heavy.
	spaced := scriptShims(t, filepath.Join(t.TempDir(), "old heavy"))
	unquoted := "case \"$PATH\" in " + spaced + ":*) ;; *) export PATH=" + spaced + ":\"$PATH\" ;; esac\n"
	kept := "export EXAMPLE=kept\n" + pathLine(foreign) + pathLine(gone) + pathLine(filepath.Join(t.TempDir(), "plan")) +
		"  " + inside + "\n" + inside + " # note\n" + "if true; then " + inside + "; fi\n" +
		"export PATH=" + old + ":\"$PATH\"\n" + unquoted
	earlier := filepath.Join(session, "sessionstart-hook-4.sh")
	writeFile(t, earlier, kept)
	notShell := filepath.Join(session, "notes.txt")
	writeFile(t, notShell, pathLine(old))
	if err := os.Mkdir(filepath.Join(session, "folder.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	status, _, _, deps := hook(t, strings.Replace(sessionStart, "%s", t.TempDir(), 1), envFile)
	if status != 0 {
		t.Fatalf("first hook = %d", status)
	}
	own := pathLine(deps.StateDirectory+"/current/shims") + sessionLine
	linkShims(t, deps.Root)

	status, stdout, stderr := hookWith(t, strings.Replace(sessionStart, "%s", t.TempDir(), 1), deps)

	if status != 0 || stdout != "" || stderr != "" {
		t.Fatalf("hook = %d, %q, %q; want 0 and nothing", status, stdout, stderr)
	}
	if got := readFile(t, earlier); got != kept {
		t.Fatalf("the earlier env file = %q, want it unchanged %q", got, kept)
	}
	if got := readFile(t, notShell); got != pathLine(old) {
		t.Fatalf("notes.txt = %q, want it unchanged %q", got, pathLine(old))
	}
	if got := readFile(t, envFile); got != own+own {
		t.Fatalf("the env file = %q, want %q", got, own+own)
	}
}

// An env file the hook cannot change fails the hook with the file; the shims of this heavy and the registration
// are written all the same, and the other files are still cleaned.
func TestAnEnvFileThatCannotBeChangedFailsTheHookWithItsPath(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes every file")
	}
	session := t.TempDir()
	old := scriptShims(t, t.TempDir())
	locked, open := filepath.Join(session, "sessionstart-hook-2.sh"), filepath.Join(session, "sessionstart-hook-4.sh")
	writeFile(t, locked, pathLine(old))
	writeFile(t, open, pathLine(old))
	if err := os.Chmod(locked, 0o444); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(session, "sessionstart-hook-6.sh")

	status, stdout, stderr, deps := hook(t, strings.Replace(sessionStart, "%s", t.TempDir(), 1), envFile)

	want := "heavy: taking the shims of an earlier heavy off PATH in " + locked + ": open " + locked + ": permission denied\n" +
		"heavy: took the shims of an earlier heavy off PATH: " + old + " in " + open + "\n"
	if status != 1 || stdout != "" || stderr != want {
		t.Fatalf("hook = %d, %q, %q; want 1 and %q", status, stdout, stderr, want)
	}
	if got := readFile(t, locked); got != pathLine(old) {
		t.Fatalf("the locked env file = %q, want it unchanged", got)
	}
	if got := readFile(t, open); got != retiredLine(old) {
		t.Fatalf("the other env file = %q, want %q", got, retiredLine(old))
	}
	if got, want := readFile(t, envFile), pathLine(deps.StateDirectory+"/current/shims")+sessionLine; got != want {
		t.Fatalf("the env file = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(deps.StateDirectory, "sessions", "4242.json")); err != nil {
		t.Fatalf("the registration: %v; want it written", err)
	}
}

// An env file that cannot be read fails the hook too.
func TestAnEnvFileThatCannotBeReadFailsTheHookWithItsPath(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads every file")
	}
	session := t.TempDir()
	locked := filepath.Join(session, "sessionstart-hook-2.sh")
	writeFile(t, locked, "export EXAMPLE=kept\n")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}

	status, stdout, stderr := start(t, filepath.Join(session, "sessionstart-hook-6.sh"))

	want := "heavy: taking the shims of an earlier heavy off PATH in " + locked + ": open " + locked + ": permission denied\n"
	if status != 1 || stdout != "" || stderr != want {
		t.Fatalf("hook = %d, %q, %q; want 1 and %q", status, stdout, stderr, want)
	}
}
