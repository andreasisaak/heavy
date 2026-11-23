package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/testkit"
)

// pathLineOf is the line with which a heavy, and other plugins, put a folder in front of PATH.
func pathLineOf(folder string) string {
	return "case \"$PATH\" in " + folder + ":*) ;; *) export PATH=" + folder + ":\"$PATH\" ;; esac\n"
}

// resumed is a session that started under heavy 0.x, went on under 1.0.x and is resumed under this heavy: the env
// files of the earlier session starts lie in its folder, under the given numbers, and Claude Code applies them all.
type resumed struct {
	session, zero, one, plan, tools string
	files                           []string
}

func newResumed(t *testing.T, zeroNumber, oneNumber string) resumed {
	t.Helper()
	r := resumed{session: t.TempDir(), zero: filepath.Join(t.TempDir(), "shims"), plan: t.TempDir(), tools: t.TempDir()}
	if err := os.Mkdir(r.zero, 0o755); err != nil {
		t.Fatal(err)
	}
	testkit.FakeProgram(t, r.zero, "heavy-shim", `echo "ValueError: unknown key red" >&2; exit 1`)
	for _, name := range []string{"uv", "make"} {
		write(t, filepath.Join(r.zero, name), "#!/bin/bash\nexec /bin/bash \"${0%/*}/heavy-shim\" \"${0##*/}\" ${1+\"$@\"}\n")
		if err := os.Chmod(filepath.Join(r.zero, name), 0o755); err != nil {
			t.Fatal(err)
		}
		testkit.FakeProgram(t, r.tools, name, `echo "the real `+name+` got $*"`)
	}
	testkit.FakeProgram(t, r.plan, "plan-tool", "echo plan")
	r.one = filepath.Join(installed(t, "1.0.4", "old"), "shims")
	zeroFile := filepath.Join(r.session, "sessionstart-hook-"+zeroNumber+".sh")
	oneFile := filepath.Join(r.session, "sessionstart-hook-"+oneNumber+".sh")
	write(t, zeroFile, pathLineOf(r.zero)+"export HEAVY_SESSION=resumed-session\n"+pathLineOf(r.plan))
	write(t, oneFile, pathLineOf(r.one)+"export HEAVY_SESSION=resumed-session\n")
	r.files = []string{zeroFile, oneFile}
	return r
}

// shell runs a script with the env files of the session applied in the order of their names, as Claude Code does
// for every Bash call, in front of a PATH that ends with the real programs.
func (r resumed) shell(t *testing.T, world herdrWorld, script string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(r.session, "*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	source := ""
	for _, file := range files {
		source += `. "` + file + `"; `
	}
	command := exec.Command("/bin/sh", "-c", source+script)
	for _, entry := range world.env {
		if !strings.HasPrefix(entry, "PATH=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "PATH="+r.tools+":/usr/bin:/bin")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("running %q with the environment of the session: %v\n%s", script, err, output)
	}
	return string(output)
}

// 2026-11-23: a session started under heavy 0.6.0 and resumed under 1.3.0 had the script shims of 0.6.0 on PATH,
// and every uv call failed in their Python code. The session start of this heavy takes the shims of every earlier
// heavy off the PATH of the session, wherever their env files stand in the order: only the shims behind the link
// stay, the folder of the other plugin stays, and uv and make reach the real programs.
func TestAResumedSessionRunsOnlyTheShimsOfThisHeavy(t *testing.T) {
	for name, numbers := range map[string][2]string{
		"earlier files first": {"4", "5"},
		"earlier files last":  {"7", "8"},
		"one on each side":    {"8", "4"},
	} {
		t.Run(name, func(t *testing.T) {
			session := newResumed(t, numbers[0], numbers[1])
			newer := installed(t, "1.4.0", "new")
			if err := os.Symlink("../bin/heavy", filepath.Join(newer, "shims", "uv")); err != nil {
				t.Fatal(err)
			}
			envFile := filepath.Join(session.session, "sessionstart-hook-6.sh")
			world := newHerdrWorld(t, "{}", "HEAVY_CLAUDE_PID=4242", "CLAUDE_ENV_FILE="+envFile)
			before := session.shell(t, world, `printf %s "$PATH"`)

			start := world.run(t, filepath.Join(newer, "bin", "heavy"), `{"session_id": "resumed-session", "cwd": "`+t.TempDir()+`"}`, "--hook", "session-start")

			want := outcome{stderr: "heavy: took the shims of an earlier heavy off PATH: " + session.zero + " in " + session.files[0] + "\n" +
				"heavy: took the shims of an earlier heavy off PATH: " + session.one + " in " + session.files[1] + "\n" +
				"heavy: CLAUDE_PLUGIN_DATA is not set, so the herdr plugin is not generated\n"}
			if numbers[0] > numbers[1] {
				lines := strings.SplitAfter(want.stderr, "\n")
				want.stderr = lines[1] + lines[0] + lines[2]
			}
			if start != want {
				t.Fatalf("session start = %+v, want %+v", start, want)
			}
			if !strings.Contains(before, session.zero+":") || !strings.Contains(before, session.one+":") {
				t.Fatalf("PATH before the session start = %q; want both earlier shims folders on it, or the test proves nothing", before)
			}
			wantPath := filepath.Join(world.state, "current", "shims") + ":" + session.plan + ":" + session.tools + ":/usr/bin:/bin"
			if numbers[0] > "6" {
				// The other plugin has its line in the file of heavy 0.x, which Claude Code applies last here.
				wantPath = session.plan + ":" + filepath.Join(world.state, "current", "shims") + ":" + session.tools + ":/usr/bin:/bin"
			}
			if got := session.shell(t, world, `printf %s "$PATH"`); got != wantPath {
				t.Fatalf("PATH after the session start = %q, want %q", got, wantPath)
			}
			if got, want := session.shell(t, world, "uv --version; make --version; plan-tool"), "the real uv got --version\nthe real make got --version\nplan\n"; got != want {
				t.Fatalf("uv, make and the tool of the other plugin = %q, want %q", got, want)
			}
			if got, want := session.shell(t, world, `command -v uv`), filepath.Join(world.state, "current", "shims", "uv")+"\n"; got != want {
				t.Fatalf("uv of the session = %q, want the shim of this heavy %q", got, want)
			}
		})
	}
}
