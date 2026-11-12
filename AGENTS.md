# heavy

Claude Code plugin and its own marketplace: one Go binary that queues the heavy commands of all sessions machine-wide behind a daemon, which admits runs as memory and load allow. User docs: `README.md`, changes: `CHANGELOG.md`.

## Rules

- Write the failing test first and see it fail for the expected reason, then the code.
- Tests use `internal/testkit`: temporary state directories (`testkit.StateDir`, short paths for the 103-byte socket limit of macOS), temporary homes, fake `docker`, `herdr` and other programs. They never touch the real queue in `~/.cache/heavy`, the real herdr or the real `CLAUDE_PLUGIN_DATA`.
- Test names describe behaviour (`TestTheDaemonIsNoChildOfTheHeavyThatStartsIt`); assertions compare whole values with `reflect.DeepEqual` or exact strings.
- Code, comments, CLI output, hook texts and skills in English; one topic per file, about 200 lines; platform code behind build tags (`*_darwin.go`, `*_linux.go`). Code that parses `/proc` or cgroup files takes the root folder as an argument and compiles everywhere, so the Mac tests cover it; only the wiring to `/proc` is tagged. A test file named `*_linux_test.go` does not run on the Mac.
- Several versions of heavy run on one machine at once: sessions keep the hooks of the version they started with, and one daemon serves them all. Every format they share is therefore read past what a version does not know, never with `DisallowUnknownFields`: the lists (`internal/lists` notes it in `Warnings` and never returns an error to a hook or shim), the protocol, the session registrations, the history, the machine configuration. A hook or shim never refuses a call over a list. A new field in a message goes only to a heavy that knows it (`Identity.Since`, as `RunStatus.Waits`), because 1.2.0 and older read the protocol strictly. Only the helper protocol stays strict, since daemon and helper are one build.
- The daemon owns the queue: other code reads it through the protocol (`client.Status`; `client.StatusIfRunning` in hooks and shims, which starts no daemon) or `heavy --status --json`.
- Shims run on every `docker`, `make` and package manager call: keep the light path free of daemon calls, git and list reads (`make --version` costs under 2.5 starts of the real `make`, `internal/shim/speed_test.go`).
- SessionStart hook stdout becomes Claude context, so it stays empty; notes go to stderr. A hook's `updatedInput` replaces the whole tool input, so it sends every field back.
- `hooks/build.sh` is POSIX sh with all logic in functions and `main "$@"; exit` as the last line, so an edit cannot change a running script; it uses `/bin/rm`.

## Commands

```bash
sh -n hooks/build.sh
jq empty .claude-plugin/plugin.json .claude-plugin/marketplace.json hooks/hooks.json monitors/monitors.json
test -z "$(gofmt -l .)"
go vet ./... && GOOS=linux go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
go test ./...
docker run --rm --init -u 1000:1000 -e HOME=/home/t -v heavy-probe-home:/home/t -v "$PWD":/src -w /src golang:1.27 go test ./...
go test -tags integration ./internal/helper/... ./internal/sampler/... ./internal/procs/...
claude plugin validate .
```

The `docker run` line runs the tests on Linux, as a user that is not root (root reads every `/proc/<pid>/environ`, so the tests of the unprivileged helper skip) and with `--init`, which reaps the daemons the tests start; the volume `heavy-probe-home` must belong to user 1000 (`docker run --rm -v heavy-probe-home:/home/t golang:1.27 chown 1000:1000 /home/t`). The integration tests need a running OrbStack and use only containers and images named `heavy-probe-*`. `claude plugin validate` warns that `CLAUDE.md` at the plugin root is not plugin context; that is expected, it serves agents working on this repository.

## Release

1. Raise `version` in `.claude-plugin/plugin.json` and add a `CHANGELOG.md` section; update `README.md` in the same commit when behaviour changes.
2. The user pushes `main`; the marketplace `heavy` has auto-update on, so sessions install the new version on their own. The first one downloads the build GitHub made of it, or builds it while that build is not there yet.
3. The running daemon hands over to the first heavy of the newer version that queues a run; waiting and running runs go over through their clients (`server.bound` names what holds it back: a paused, restarting or just starting run). The hooks of the newer version turn the link `$STATE/current`, behind which the shims of every session lie (`internal/current`). A daemon of 1.2.0 or older still hands over only without runs, and sessions started before 1.3.0 keep their versioned `PATH`.

## Architecture

`cmd/heavy` dispatches on its name and arguments: `heavy <command>` queues a run, `heavy --hook guard|stop|session-start` are the hooks, `heavy --monitor` the plugin monitor, `heavy --daemon` the daemon, `heavy --helper` the side in the Docker VM; under any other name (`shims/make` and the like, symlinks to `../bin/heavy`) it acts as that program's shim.

A heavy call flows hook → client → daemon → scheduler → process control:

| Package | Role |
|---------|------|
| `internal/guard` | PreToolUse hook for Bash: finds heavy commands, puts `heavy` in front, sends the call to the background, denies subagent runs and polls of the session's own run |
| `internal/stophook` | PreToolUse hook for TaskStop and KillShell: speed bump before stopping a run |
| `internal/sessionstart`, `internal/sessions`, `internal/current` | SessionStart hook: shims and `HEAVY_SESSION` into `CLAUDE_ENV_FILE`, registration `$STATE/sessions/<claude-pid>.json`; the link `$STATE/current` to the plugin folder of the newest heavy, whose `shims/` is on `PATH` |
| `internal/shim` | the shims: real program for light calls, `HEAVY_RUN`/`HEAVY_CPUS` injection into docker calls inside a run |
| `internal/shell`, `internal/catalogue`, `internal/detect`, `internal/label`, `internal/lists` | Bash parsing on `mvdan.cc/sh/v3`, the catalogue of runners, matching against `.heavy.json` and the personal list, the short name of a run |
| `internal/client` | `heavy <command>` and the CLI options: registers, waits, runs the command in its own process group, watches `red` patterns, re-announces a run to a new daemon |
| `internal/protocol`, `internal/daemon` | JSON-lines protocol v1 over `$STATE/daemon.sock`; the daemon samples every 2 s, decides, and runs the actions in one job loop |
| `internal/scheduler` | pure `Decide(state, measurements, config, now)`: start, pause, resume, restart; `Holds` names what keeps each waiting run, from the same conditions |
| `internal/history` | run history per day under `$STATE/history/`, the expected peak of a command |
| `internal/sampler`, `internal/helper`, `internal/procs`, `internal/dockercli` | host measurement (`sysctl`, `vm_stat` on a Mac, `/proc` on Linux), the helper container `heavy-helper:<stamp>` built from `bin/heavy-linux` on a Mac and the same helper inside the daemon on Linux (`sampler.Local`), process groups, tagged VM processes and containers of a run, docker calls with a deadline |
| `internal/cgroup`, `internal/procfs`, `internal/platform` | the memory the cgroup v2 limits of the daemon's user leave, `/proc/<pid>/stat`, and the word for where docker runs (`VM` or `machine`) |
| `internal/slots` | the machine configuration `/etc/heavy/config.json` and the slot files that limit the runs of all users of a machine |
| `internal/foreign` | notices about big containers outside the runs under memory pressure |
| `internal/monitor` | `heavy --monitor`: finds its session through the Claude Code ancestor, subscribes to the daemon, prints the events of its session |
| `internal/herdr` | the herdr plugin `local.heavy-queue`: manifest, refresh in every running herdr server, forward |
| `internal/statusview`, `internal/dashboard` | `heavy --status` and `heavy --dashboard` (bubbletea v2) |
| `internal/testkit` | test helpers |

`hooks/build.sh` puts `bin/heavy`, and on a Mac `bin/heavy-linux`, in place when the stamp (SHA-256 of the Go sources, `bin/.stamp`) differs, under a lock file, then execs the binary: it downloads the release `build-<stamp>` that the job `release` of `.github/workflows/test.yml` builds after the checks of a push to `main`, and builds with the local Go when there is none, as in a checkout with changes. `sh hooks/build.sh stamp` prints the stamp.

## Gotchas

- `mkdir` is no lock: on the Ubuntu runner of the CI two processes that created the same folder at once both got a zero status (2026-11-11). `hooks/build.sh` takes its lock by creating a file with `set -C` (noclobber), which the shell opens exclusively.
- On Linux each user has a daemon of their own (`~/.cache/heavy`), which reads and signals only what that user may. Rootless docker runs the non-root users of a container under subordinate user ids: the daemon skips such processes (`helper.Options.Unprivileged`) and reaches them through `docker pause` of their container.
- Linux refuses `O_CREAT` on the file of another user in a sticky folder (`fs.protected_regular`), so `internal/slots` opens a slot file first and creates only a missing one.
- A daemon learns of the runs of other users only through the slots they hold; without a machine configuration it knows none.
- In an interactive session Claude Code takes the Bash environment before the SessionStart hooks end, so `build.sh` writes the shims and `HEAVY_SESSION` into `CLAUDE_ENV_FILE` before any build. `claude -p` waits for the hooks.
- `claude --resume` applies the env files of earlier SessionStart hooks again, also of a plugin that is now disabled, and a session never gets a new `PATH`. So the shims are on `PATH` behind the link `$STATE/current`, and nothing that must change with an update belongs into the env file.
- Claude Code starts no plugin monitors with `claude -p` and does not restart them on `/reload-plugins`; the monitor's stderr reaches no model, so everything the session must hear goes to stdout.
- Claude Code ends a stopped task together with every process below it by parent PID. `daemon.Connect` therefore starts the daemon through `heavy --daemon --detach`, so it hangs on launchd.
- The kernel name of native Claude Code is its version (such as `2.1.319`); the monitor finds it by `argv[0]` from `kern.procargs2`, not by `p_comm` (`internal/monitor/process_darwin.go`).
- A daemon built in this repository serves every session on the machine, and its session start turns `~/.cache/heavy/current` to this checkout. Develop with `claude --plugin-dir . --settings '{"enabledPlugins":{"heavy@heavy":false}}'` and use `HEAVY_STATE_DIRECTORY` for probes that should not reach the real queue.
- The herdr plugin link is global for all herdr servers (`herdr --session <name>` included); relinking it to another heavy changes the sidebar of every session.
- Under load, `docker inspect` over all containers takes longer than the 5-s deadline; `SystemDocker` therefore inspects only containers it has not seen within 30 s.
