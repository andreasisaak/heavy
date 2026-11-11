# Changelog

All notable changes to this project are documented in this file. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.0] - 2026-11-11

### Added

- heavy runs on Linux. Docker needs no VM there, so the daemon reads the machine itself from `/proc`, without a helper container, and finds the processes of a run in its containers with the rights of its user; rootless docker works. `hooks/build.sh` builds only `bin/heavy` on Linux and stamps with `sha256sum` where `shasum` is missing.
- On Linux the available memory is at most what the cgroup v2 limits of the user leave (`memory.high` and `memory.max` less the memory in use, which is `memory.current` without the file cache), so a user slice with `MemoryHigh` is not overbooked while the machine has memory left.
- A machine configuration `/etc/heavy/config.json` with `max_parallel` and `slots` limits the runs of a whole machine over all its users. Each user keeps a queue and a daemon; each daemon locks one slot file per active run. A run without a free slot waits and says so.
- Under a machine configuration the runs of other users count as load: a run starts unchecked only while no run is active on the whole machine.
- A machine needs no Go any more: GitHub builds the binaries of each pushed state as the release `build-<stamp>`, named after the SHA-256 of the sources, and `hooks/build.sh` downloads them at a session start and checks their SHA-256. Without a release for the sources, or without `curl`, heavy builds itself with the local Go as before. `HEAVY_RELEASES` names the releases of a fork.
- The session monitor reports the start of a run, its first red line and its end with the exit status, so a session needs no monitor or watch loop of its own; the context of a queued call says so.

### Changed

- The hook puts `heavy` in front of a heavy command also in a call of several lines, with comments or with heredocs, and inside `( … )`. The work before it runs at once in the same shell and only the heavy command waits. Before, such a call ran through heavy as a whole, or was denied when other work stood before the heavy command.
- Several heavy commands behind other work, such as `git add -A && make ci && make test`, queue each on their own instead of being denied.
- The context of a queued call says "this machine" instead of "this Mac".
- With a machine configuration its `max_parallel` stands in for the default of three runs per user; a `max_parallel` in the personal list still lowers it.
- The hint for a missing Go names the build on GitHub and <https://go.dev/dl/> beside `brew install go`.
- `hooks/build.sh` says so when it takes over the build lock of a process that no longer runs.
- On Linux the pause lines, the notice about low memory and the dashboard say `machine` where a Mac says `VM`.

## [1.0.4] - 2026-11-09

### Fixed

- Under heavy load the memory of a run's processes in the Docker VM no longer goes unmeasured. heavy inspected every container of the machine at each sample, which took longer than 5 seconds under load 30; it now asks `docker ps` for the containers and inspects only new ones, and each container again after 30 seconds.

## [1.0.3] - 2026-11-09

### Changed

- heavy expects the memory of a run from the earlier runs of the same command, and only for a new command from the runs with the same short name. Before, a measurement on seven test servers named `test-complete` made heavy expect 10.7 GiB for every `make test-complete TEST_WORKERS=1`, so those never started beside another run.
- A waiting run that does not fit beside the active ones lets the smaller runs behind it pass, until it has waited 15 minutes. Before, it kept every run behind it waiting, even runs that would have fit.

## [1.0.2] - 2026-11-09

### Added

- The session monitor tells the session when the daemon stays out of reach for a minute, and again when it answers. Its runs keep running meanwhile; a shorter loss, such as the handover to a newer heavy, stays silent.

## [1.0.1] - 2026-11-09

### Fixed

- The herdr sidebar shows the heavy runs in every running herdr, the default one and each `herdr --session <name>`. Before, the daemon refreshed only the herdr it was started from, so the panes of the other herdr sessions showed no heavy rows.

## [1.0.0] - 2026-11-08

### Changed

- heavy is one Go binary instead of Bash and Python. The hooks, the shims and `heavy` itself build on the first session after an install or an update with the local Go 1.27 (`hooks/build.sh`); without Go the hooks stop with a message that says how to get it. The session start puts the shims on PATH before it builds, because Claude Code takes the environment of an interactive session before its hooks end. After the update, start new sessions instead of resuming: a resumed session keeps the PATH of 0.6. A build that fails keeps the binary of the last build in use and says so; without any binary the Bash and TaskStop hooks block the call instead of letting it through unchecked. Python and `jq` are no longer needed at run time.
- A daemon on demand replaces the machine-wide lock. It admits up to three runs at once, one per git worktree, when the memory a run used in its last five runs fits into the Docker VM and the Mac; a run without such a record, and any run beside which nothing is active, starts alone. Under memory pressure it pauses the youngest run, resumes it after recovery, and ends and requeues a run whose entry allows `restart`. `HEAVY_CPUS` tells a run how many cores it may use.
- `heavy --status` shows the id and the memory of each run and, while the Docker VM is out of reach, a line `VM: not measured (…)`; `--status --json` adds `id`, `checkout`, `budget`, `rss_bytes`, `predicted_bytes`, `paused_reason` and `restarts`, and the state `paused`.
- The herdr sidebar shows each run of an agent in a row of its own (tokens `heavy`, `heavy_2`, `heavy_3`, from four runs on `+n more`), a paused run as `⏸`. Run `/heavy:herdr` again to add the rows and the key `prefix+d` for the dashboard. It replaces the herdr link of 0.6 on its own and asks one yes/no question before it changes your herdr config.
- The texts of the hooks, the shims and `heavy --status` speak of the queue and the room on this Mac instead of the one lock: a heavy command starts as soon as the Mac has room for it.
- Inside a run, the docker shim adds `-e HEAVY_RUN=<id> -e HEAVY_CPUS=<n>` to `docker exec`, `docker run` and `docker compose exec|run`, so heavy finds, measures, pauses and ends the run's processes in the VM through a helper container.

### Added

- `heavy --pause <id>`, `--resume <id>` and `--stop <id>`.
- `heavy --dashboard`, a live view of the machine, the runs, the memory of the last 30 minutes, the biggest containers outside the runs and the last results, with keys to move, pause, resume and stop runs; in herdr, the plugin action `dashboard` opens it as a split pane.
- A plugin monitor (`heavy --monitor`) tells each interactive session when one of its runs is paused, resumed or restarted, and, under memory pressure, which container of its repository (main checkout or any worktree) outside the runs uses at least 1 GiB. Without a connected monitor the hook's context says how to start one.
- List entries take `red` (patterns that end a run at the first matching line of its output, exit status 1), `exclusive` and `restart`; the personal list takes the thresholds of the queue in a `load` section.

### Removed

- Python and the Bash scripts of the herdr plugin: heavy generates the herdr plugin itself (`heavy --herdr-refresh`, `heavy --herdr-forward`) in `${CLAUDE_PLUGIN_DATA}/herdr/heavy-queue`, so its link never moves again; `hooks/refresh_link.sh` is gone.
- `heavy --inside-run` and `HEAVY_HOLDER`: a shim checks `HEAVY_RUN` with the daemon.

## [0.6.0] - 2026-10-31

### Added

- Exceptions for list entries: an entry can be an object `{"command": "checkall.sh", "unless": ["CHECKALL_CASES="]}`, in `.heavy.json` and in the personal list, and does not match a call that carries one of the words in `unless`. A word ending in `=` stands for an assignment of that variable with a value that is not empty (`CHECKALL_CASES=` alone stays heavy); any other word must appear exactly (`TEST_WORKERS=12` frees `make test-complete TEST_WORKERS=12`, not `TEST_WORKERS="1 12"`). A call carries a word before its program, also behind `env`, `nice` and `timeout`, among its arguments, and, for a call a shim catches, in its environment, so `TEST_WORKERS=12 make test-complete` stays free behind a script as well. In shopfront, 50 full runs of `tests/Complete/checkall.sh` on 30 and 31 October waited 31 minutes each on average for 7 minutes of work, 42 of them from one session that only wanted to see three new cases red and green; a run of chosen cases on a test server of its own takes about a minute and now starts at once, for subagents too. The form keeps a plain string for the common entry and puts the exceptions next to the command they belong to, so one look at an entry shows when it does not hold.

### Changed

- `split_commands` of `hooks/shell_tokens.py` can keep the environment assignments and wrappers in front of each program (`normalized=False`); the guard and the detection read the exceptions from them.

## [0.5.2] - 2026-10-31

### Fixed

- The hooks took a run of another session in the same checkout for the session's own run: a session in the main checkout of shopfront had its `heavy --status` denied as "your heavy run" while another session's `make ci` ran there. A run now belongs to the Claude Code session that started it. The SessionStart hook exports the session id as `HEAVY_SESSION`, `heavy` records it as `session`, and the poll denial and the stop speed bump match it against the `session_id` of the hook input.

## [0.5.1] - 2026-10-31

### Fixed

- The speed bump for `TaskStop` said a waiting run needs no restart after new edits. That holds for a fix the run should check, not for code of the next task, which the run would check along. It now names the fix.

## [0.5.0] - 2026-10-31

Agents waited for their runs instead of coding. In the sessions of 30 and 31 October the main agents sat idle for 71 % of the time their own run waited or ran (16.8 of 23.8 hours), and 40 subagents started 128 of 429 heavy calls.

### Added

- Subagents start no heavy run: a Bash call with `agent_id` in the hook input that would queue is denied, and the subagent names the command in its report for the main agent. Subagents had spent 9.5 of their 26.7 hours in heavy calls; one waited 73 minutes in 14 calls for 1.4 minutes of runs.
- `| tail`, `| head` and `| grep` at the end of a heavy run's pipeline go before the call is queued, at any depth of `( … )`, so the user can follow the run's output live; the context names what went. 105 of 399 heavy calls had hidden their output that way, and `| head` could end a run early. A `grep -q` or `-c`, a `tail -f` and a filter that writes to a file or feeds a further stage stay.
- `heavy --status` while the session's own run waits or runs is denied with the state of that run. Sessions had polled the queue 168 times.
- A speed bump for `TaskStop` and `KillShell`: the first stop of a heavy run that waits or runs is denied with what it costs, the same stop within ten minutes passes. 16 runs had been stopped, among them three `make ci` after 12 to 17 minutes, and waiting runs to start them again at the end of the queue.

### Changed

- The context of a queued call asks the agent to keep working on files the run does not read and to end its turn only when no such work is left. Before, it told the agent not to change files the run checks, which for `make ci` meant all of them.
- `heavy --agent` names the agent of a session started with `claude --agent`; subagents no longer start runs.

## [0.4.1] - 2026-10-31

### Fixed

- `phpstan analyse --no-progress app/Cart.php` checks one file, yet queued as a check of the whole project: a file right after a long option counted as that option's value. The phpstan step of `make lint FILE=…` in shopfront waited minutes behind test runs. phpstan now has the list of its options that take a value, from `phpstan analyse --help`; every other option is a flag. `-a tools/autoload.php` stays a value and no target.

## [0.4.0] - 2026-10-31

### Added

- `heavy --forward <position>` moves a queued run one place ahead, for example from `waiting 3` to `waiting 2`. The herdr plugin gets the action `forward`, which `/heavy:herdr` binds to `prefix+f`: it moves the queued run of the focused pane ahead and says so in a notification.

### Changed

- Callers wait in queue order: the order of their waiting records, the time they started to wait unless a move changed it. Only the first caller tries the lock, once a second; before, the kernel picked the next caller. A caller of an older heavy still takes the lock the kernel way until its session reloads.
- `heavy --status` reads all records with one `jq` and one `ps` instead of five programs per record, because waiting callers read the queue every second; with 22 stale records it had taken more than a second.

- `eslint` and `stylelint` given a list of files stay free at any length; they are heavy for a directory, a glob or no target. Their runs over 4 and 30 files took 4 and 5 seconds and had waited behind test runs. A glob such as `'src/**/*.ts'` had counted as one file and stayed free.
- Several heavy commands in one call run in one hold of the lock. `make test-complete && tests/Complete/checkall.sh` had queued twice, and another run could slip in between.
- A call the hook can only run through `heavy` as a whole, and that puts other work before its heavy commands, is denied with a note to split it: run that work first in a call of its own, then the heavy commands. Sessions had bundled file edits with `python3` and a syntax check with a test run in one call; the edits waited up to 20 minutes for the lock, and the sessions stopped and restarted the waiting tasks. Reading, printing and work after the heavy commands stay in the call.

## [0.3.2] - 2026-10-30

### Fixed

- `bash -n run.sh`, `sh -n`, `bash -o noexec` and `bash -n -c '…'` only check the syntax of a script and run none of it, yet a list entry for the script queued them: a syntax check of `checkall.sh` waited minutes behind a real run. They now count as light.
- `heavy --status` cuts a command longer than 60 characters, so a run with a long file list no longer stretches every line of the table.

## [0.3.1] - 2026-10-30

### Added

- The herdr row names the subagent that started a run: `▶ 6m · checkall.sh ↳ test-author`. A subagent's run had shown on its agent's row as if the agent itself ran it. The hook passes the subagent's type (`agent_type` of the hook input) to `heavy` as the new option `--agent`, also behind a `heavy` the subagent types itself, and the records carry it as `agent`.

- `heavy --help` prints the usage.
- The herdr link moves to a new version with the first Bash call after `/reload-plugins`, not only at the next session start. Claude Code raises no event on a reload, so the guard checks a marker that `hooks/refresh_link.sh` writes and starts that script in the background while herdr may still run an older version. The link only moves forward, and the script redraws the sidebar right after it moved it. Running `/heavy:herdr` after an update is not needed.

### Changed

- A finished run stays in `heavy --status` and as `✓` or `✗` in the herdr row for 30 seconds instead of ten minutes. The sidebar redraws within a second of every change, so a result does not need to stay longer to be seen.
- A call that starts `heavy` itself, such as `heavy make test-complete`, goes to the background with the same context as a heavy command the hook found. Sessions had started such calls in the foreground, where a long wait for the lock runs into the Bash timeout, and polled `heavy --status` every minute instead of waiting to be woken.
- `heavy` refuses a command it cannot find before it waits, and points to `heavy --status`. `heavy status` without dashes had queued a command `status`, which waited for the lock and then failed.
- The check of `/heavy:herdr` starts no test run while another run waits or runs, and checks that run instead. Its `heavy sleep 20` had queued behind real runs in three sessions at once.

### Fixed

- A step of a run started by a `heavy` without `HEAVY_HOLDER`, such as an older version earlier on `PATH`, waited in a shim for the lock its own run held, until Claude Code stopped the call. `heavy --inside-run` now also finds the running run among the parent processes of the call, and a shim asks it before it queues a heavy call.
- The hook sent `heavy status` and `heavy help` to the background as runs; they now pass, and `heavy` names the right form at once.
- A record of a killed caller whose pid macOS had handed to a younger process counted as a live run, so the queue showed a stale `status` from five days earlier and every waiting position one too high. A record now counts only while its process started no later than the record.

## [0.3.0] - 2026-10-28

### Added

- A catalogue of heavy runners: in a project with heavy on, every full test run, type check of the whole project and build queues without an entry, through any launcher (`docker compose exec web pnpm exec vitest run`, `docker exec … sh -c '…'`, `uv run pytest`, `php artisan test`, `pnpm typecheck`). Sessions had started these steps directly instead of the listed make targets, and they ran without `heavy`. A run that names one to three source files, and a run that never ends, stays outside the queue. A source file right after a long option (`--setupFiles tests/setup.ts`) is that option's value and makes no run a single-file run; a script with a suffix such as `:watch`, `:ui` or `:open` counts as never ending.
- Shims for `docker`, `make`, `gmake`, `pnpm`, `pnpx`, `npm`, `npx`, `yarn`, `bun`, `bunx`, `uv`, `uvx` and `composer`, which a new SessionStart hook puts in front of `PATH` for every Bash call of a session. A heavy call that reaches them without `heavy`, hidden from the hook by a variable, a script file or a make target, waits there for the lock.
- `heavy --inside-run`, and `HEAVY_HOLDER` in the environment of every run, so a shim lets the steps of a running run pass without waiting again.
- The project of a call also comes from `pnpm -C`, `uv --directory`, `composer -d`, the `--project-directory` or `-f` of `docker compose`, `COMPOSE_FILE`, and for `docker exec <container>` from the compose label of the container. A directory with a variable the shell still has to expand (`$D`) keeps the directory known before it.
- Commands in backticks and in `$( … )` inside double quotes count as commands of the call; inside single quotes or escaped they stay text, so a commit message that names `make ci` passes.
- Each record carries the short name of its run (`label`: `ci` for `make ci`, `vitest`, `checkall.sh`), from the new `hooks/heavy_label.py`.
- A run that ended stays in `heavy --status` for ten minutes with its end and exit status (`passed`, `failed 2`).
- `heavy` redraws the herdr sidebar every 30 seconds while a caller waits or runs (`HEAVY_TICK_SECONDS`).

### Changed

- A personal entry turns heavy on for its project without a `.heavy.json`, and `"enabled": false` also turns the catalogue off.
- The hook no longer denies a heavy command it cannot place. A wrapper such as `nice -n 10`, `env` or `timeout` and a `bash -c '…'` get `heavy` in front; parentheses, `$( … )`, backticks, heredocs, comments and line breaks run with the whole call through `heavy "$SHELL" -c '…'`. Each denial had sent sessions looking for another form.
- The additional context says that a run naming one to three test files stays outside the queue.
- The herdr row is a symbol, the minutes so far and the short name of the run: `▶ 6m · ci` while it runs, `⧗ 3m · #1 · ci` while it waits, `✓ 13m · ci` or `✗ 13m · ci` for ten minutes after it ended. The row had shown the whole command and its start time (`env CHECK_WORKER=1 tests/Complete/checkall.sh running since 21:02`), which the narrow sidebar cut off. The colour rules of the row match the symbol; run `/heavy:herdr` again to replace the old row.

## [0.2.2] - 2026-10-15

### Fixed

- A heavy command behind a shell keyword ran without `heavy`: `if make ci; then …`, `for … do make ci; done`, `while ! make ci; …`, `until make ci; …`, `! make ci` and `{ make ci; }` are now queued.
- `timeout 900 make ci` ran without `heavy`, because the duration counted as the program. It is now denied like the other wrappers.
- A foreground `make ci &` was rewritten into invalid bash (`{ heavy make ci &; }`) and failed with a syntax error. A test now runs rewritten commands with bash and the real `heavy`.

## [0.2.1] - 2026-10-15

### Fixed

- A background heavy command ran without `heavy` when another hook rewrote the same call at the same time, for example `make ci > ci.log 2>&1` turned into a `tee` pipeline (0 of 5 calls kept `heavy` next to such a hook). The hook now denies the three forms such hooks rewrite (stdout into a file, `tee` into a missing directory, a trailing `echo "… $?"` without `exit`) and names the `tee` form they leave alone (10 of 10 calls kept `heavy`).

## [0.2.0] - 2026-10-15

### Changed

- An entry's program counts by its file name, and a script that `sh` or `bash` starts counts as that script, so one entry such as `infra-test.sh` queues `tools/ci/infra-test.sh`, `./tools/ci/infra-test.sh` and `bash tools/ci/infra-test.sh`.
- The hook runs `/usr/bin/python3` instead of the first `python3` on the `PATH`, which saves about 90 ms per Bash call behind a version manager shim.
- The hook reads the queue from `heavy --status --json` instead of the state files, so it reports the same queue as `heavy --status`.
- The SessionStart hook links the herdr plugin again only from another version of the installed plugin; a link from a clone of the repository stays.

## [0.1.0] - 2026-10-15

### Added

- `heavy` command: one machine-wide lock for heavy commands, waiters in arrival order, `--status` and `--status --json`, `--directory`.
- PreToolUse hook that puts `heavy` in front of the commands a project lists in `.heavy.json` or in the personal list `~/.config/heavy/config.json`, and sends the call to the background.
- `/heavy:setup` skill that proposes the heavy commands of a project, including the direct calls of the steps behind its heavy targets, and writes the list.
- herdr plugin `local.heavy-queue`, `/heavy:herdr` skill, and a SessionStart hook that links the plugin again after an update.