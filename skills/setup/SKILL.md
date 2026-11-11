---
name: setup
description: Set up the heavy queue for the current project. Finds the commands that load the whole machine (CI, full test suites, coverage, end-to-end and UI tests, full checks) in the Makefile, package.json, composer.json, pyproject.toml and the agent docs, proposes them, and writes .heavy.json or the personal list ~/.config/heavy/config.json after the user agrees. Use it when the user runs /heavy:setup, wants to add a project to heavy, asks which commands of a project should queue behind heavy, or wants to create or change a .heavy.json, even if they do not name the skill.
---

# heavy: set up a project

heavy runs heavy commands through a machine-wide queue on this machine: up to three at once, one per git worktree, as memory and load allow. Once a list names the project, the plugin's PreToolUse hook puts `heavy` in front of every listed command and of every full test run, type check and build of its catalogue, and sends the call to the background; shims catch at run time what the hook does not see. This skill finds the commands the catalogue cannot know for the current project, proposes them and writes the list once the user agrees. An empty list (`{"commands": []}`) already turns the catalogue on.

## 1. Find the checkout

Run `git rev-parse --show-toplevel` and `git rev-parse --path-format=absolute --git-common-dir`. The first is the checkout root, where `.heavy.json` lives. The parent of the second is the main checkout, the key of the personal list; it is the same for every worktree of the repository.

Outside a git repository, stop and say so: heavy finds a project only through its git checkout.

## 2. Read what is listed already

- `<checkout root>/.heavy.json`: `{"commands": ["make ci", "make test"]}`
- `~/.config/heavy/config.json`, the entry of the main checkout: `{"projects": {"~/sites/example": {"enabled": true, "commands": ["make e2e"]}}}`

Show what is there. `"enabled": false` switches the project's `.heavy.json` off on this machine; the personal `commands` still count.

## 3. Collect candidates

Read only the files that exist in the checkout root:

| Source | What to read |
|--------|--------------|
| `Makefile` | targets with a `##` help comment, plus targets named like ci, check, test, coverage, e2e |
| `package.json` | `scripts`; in a workspace also the `package.json` files the root scripts call |
| `composer.json` | `scripts` |
| `pyproject.toml` | `[tool.pytest.*]`, `[project.scripts]`, task runners such as `[tool.poe.tasks]` |
| `AGENTS.md`, `CLAUDE.md` | the commands they tell agents to run before a commit or for tests |

The agent docs decide the spelling: a session types what they say, so `make test` where the docs say `make test`, `npm run test:e2e` where they say that. A command that the docs do not mention and that only wraps a listed one (a `package.json` script behind a Makefile target) is no entry of its own; the direct forms below are the exception.

### Follow the heavy targets

Sessions do not always type the target. To rerun one failed part, or after reading a script, they start a step behind `make ci` directly. The catalogue already queues the steps that name their runner (`docker compose exec app vendor/bin/phpunit`, `pnpm exec vitest run`, `uv run pytest`), so do not list those. For each target you will propose, read its recipe and follow it into the scripts it runs inside the repository, about two levels deep (`make ci` → `tools/ci/local-ci.sh` → `tools/ci/infra-test.sh`). For each step that is heavy on its own and hides its runner, propose an entry for the form a session would type:

| Step in the recipe or script | Entry | Queues |
|------------------------------|-------|--------|
| a CI script, `tools/ci/infra-test.sh` | `infra-test.sh` | `tools/ci/infra-test.sh`, `./tools/ci/infra-test.sh`, `bash tools/ci/infra-test.sh`, `sh -x tools/ci/infra-test.sh` |
| a test container, `docker run --rm … shopfront-infra-test …` | `docker run shopfront-infra-test` | every `docker run` of that image, whatever the options |
| a compose service, `docker compose run --rm e2e …` | `docker compose run e2e` | every run of that service |

The hook compares programs by file name and counts a script that `sh` or `bash` starts as that script, so one entry per script covers every way to start it; write it as its file name (`infra-test.sh`). A script that runs an image through a variable (`"$image"`) still gets an entry: use the tag it builds (`docker build --tag shopfront-infra-test`), because that tag is what a session copies into its call. A compose service or image whose default command runs the tests needs an entry too, because the call names no runner. Leave out steps that are light on their own.

## 4. Choose the heavy ones

A command is heavy when it keeps many cores busy for a minute or more:

- CI pipelines and local gates (`make ci`, `make check`, `composer check`)
- full test suites, with or without coverage (`make test`, `pytest`, `npm run test`)
- end-to-end, browser and UI tests, screenshot comparisons
- full checks that chain lint, types, tests and build

Leave these out, and say why in one line each:

- commands that never end on their own: dev servers, watchers, `logs -f`, Storybook, `docker compose up`. A run keeps its place in the queue until the command ends, so one of them would block a worktree and a place for every other session.
- commands for one file or one test (`make test-file FILE=…`, `make lint-file`): they are fast and the sessions run them often. A runner call that names one to three source files stays outside the queue on its own.
- direct runner calls, type checks and builds (`vendor/bin/phpstan analyse`, `pnpm typecheck`, `pnpm build`): the catalogue queues them. A make target that chains them, such as `make lint`, still needs an entry when it is heavy, because the hook sees only `make lint`.
- format on its own: it is fast.

How the hook matches an entry: the program must be the same and the remaining words of the entry must appear in the call's arguments in order. So `make test` also queues `make test FILE=x`, but not `make test-file`. Name the catalogue in one line when you propose, so the user sees why direct runner calls are missing from the list.

## 5. Propose

Show a table: command, source (`file:line`), why it is heavy. Below it the commands left out, each with its reason. Then ask where the list goes:

- `.heavy.json` in the checkout root (default): travels with the branch, so it holds in every worktree that has the commit, and other people get it. Suggest committing it; do not commit yourself.
- the personal list: only for you on this machine, for all worktrees of the main checkout, nothing to commit. Fits when the user does not want to change the repository.

Write nothing until the user agrees. If they change the selection, show the final list again before writing.

## 6. Write

- `.heavy.json`: `{"commands": [...]}` with exactly that key; each entry a non-empty string, or an object `{"command": "checkall.sh", "unless": ["CHECKALL_CASES="]}` when a light form of the command exists that the docs name, such as a run of chosen cases or of one file. A word in `unless` ending in `=` stands for an assignment of that variable with a value; any other word must appear exactly. If the file exists, show the change against it first.
- Personal list: read `~/.config/heavy/config.json` if it exists and keep every other project. Key the entry with the main checkout path, `~` for the home directory. Leave out `"enabled"` unless the user wants the project's `.heavy.json` switched off. Create `~/.config/heavy/` if it is missing.

A list of any other shape makes the hook deny every Bash call in that project with `heavy: <file>: expected …`, which names the file and what it expected.

### Entry fields

An entry can be an object with more fields when the project gives a reason for them. Propose each one with that reason in one line, and leave it out without one:

- `red`: patterns (RE2) of the first line of output that already means the run failed. heavy then ends the run at that line with exit status 1, instead of letting a red CI run on for minutes. Look for what the project prints itself: a script that reports the first failure (such as `tools/ci/first-red.sh` printing `First red in …`), a runner summary line (`^FAILURES!$`, `^FAIL `). Only entries with `red` read the output line by line.
- `exclusive`: the run starts only alone and nothing starts beside it. Use it for runs that need the whole machine or a shared resource another run would disturb, such as browser tests against one fixed port or a database the suite resets.
- `restart`: heavy may end the run under lasting memory pressure and start it again as the next run. Use it only for runs that can start over without harm: no deploys, no migrations against shared data, no runs that write outside the checkout.

```json
{"commands": [
  {"command": "make ci", "red": ["^First red in "]},
  {"command": "make e2e", "exclusive": true},
  {"command": "make test", "restart": true}
]}
```

heavy tells each run how many cores it may use in `HEAVY_CPUS`, also inside containers started through `docker compose exec` or `docker run`. When a Makefile or script sets its parallelism from the machine (`sysctl -n hw.ncpu`, `nproc`, a fixed `-j`), suggest reading `HEAVY_CPUS` first, for example `JOBS ?= $(or $(HEAVY_CPUS),$(shell sysctl -n hw.ncpu))`, so parallel runs share the cores instead of each taking all of them. Name the line (`file:line`); change it only when the user agrees.

## 7. Check

Ask the hook directly with one listed command, so a mistake shows now and not during the next CI run:

```bash
printf '{"tool_name":"Bash","cwd":"%s","tool_input":{"command":"%s"}}' "<checkout root>" "<listed command>" | sh "${CLAUDE_PLUGIN_ROOT}/hooks/build.sh" guard
```

The output starts with `{"hookSpecificOutput"` and its `additionalContext` begins with `Queued behind heavy`. No output means the command does not match; a denial whose reason starts with `heavy: <file>: expected` names the broken file. Report the result in one line.
