@AGENTS.md

## Claude Code

- Test the plugin from this checkout in a separate session: from the repository root, `claude --plugin-dir . --settings '{"enabledPlugins":{"heavy@heavy":false}}'`. It builds `bin/heavy` at its start; the installed plugin stays off for that session.
- This repository has no heavy list, so its own `go test ./...` runs outside the queue; start the checks with `run_in_background`.
- The plugin monitor of a session started with `--plugin-dir` runs this checkout's `bin/heavy`; after a rebuild, start a new session to get the new monitor.
