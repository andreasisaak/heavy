---
name: herdr
description: Show the heavy queue in the herdr sidebar. Links the herdr plugin local.heavy-queue that heavy generates, and adds three $heavy rows to the agent rows of ~/.config/herdr/config.toml, so every agent row shows each of its heavy runs in a line of its own with a symbol, the minutes so far and a short name, such as "▶ 6m · ci", "⏸ 4m · test" or "⧗ 3m · #1 · test". Use it when the user runs /heavy:herdr, wants to see heavy runs in herdr, asks why the heavy tokens do not show in the herdr sidebar, or sets up heavy together with herdr, even if they do not name the skill.
---

# heavy: show the queue in herdr

heavy generates the herdr plugin `local.heavy-queue` at every session start, in `${CLAUDE_PLUGIN_DATA}/herdr/heavy-queue`. It reports each heavy run of an agent's pane as a pane token of its own: `heavy`, `heavy_2`, `heavy_3`, and from four runs on the third says `+<n> more`. herdr shows a custom token only where the sidebar rows in its config name it, so the setup has three parts: link the plugin once, add the rows, bind the keys.

## 1. Check herdr

Run `herdr --version`. The plugin needs herdr 0.9.1 or newer. Without herdr, or with an older one, stop and say so: heavy itself works without herdr, only this display is missing.

## 2. Link the plugin

The folder is `${CLAUDE_PLUGIN_DATA}/herdr/heavy-queue`. If that text was not replaced by a path, find it with `ls -d ~/.claude/plugins/data/heavy*/herdr/heavy-queue`. It must hold `herdr-plugin.toml`; if it does not, the session start could not write it, and its stderr note in the session start says why. Resolve the folder with `cd … && pwd -P`, because herdr stores resolved paths.

Run `herdr plugin list --plugin local.heavy-queue --json` and compare `.result.plugins[0].plugin_root` with that folder:

- No plugin listed: `herdr plugin link "<folder>" --enabled`.
- Same folder: nothing to do.
- A folder of heavy 0.6, `${CLAUDE_PLUGIN_ROOT}/herdr/heavy-queue` or a version folder of the plugin cache such as `~/.claude/plugins/cache/heavy/heavy/0.6.0/herdr/heavy-queue`: replace it without asking, `herdr plugin unlink local.heavy-queue`, then link as above.
- Any other folder may be the user's own copy: name it and ask in one plain sentence whether heavy may replace it. On yes, unlink and link as above.

Options go after the path; `herdr plugin link --enabled <path>` fails with "unknown option". The link never has to move again: every newer heavy rewrites the manifest in this folder at its session start, and after `/reload-plugins` at the first Bash call, with the commands of its own `bin/heavy`. An older heavy never overwrites a newer manifest. Do not run this skill for an update.

## 3. Add the sidebar rows

Three rows, one per token, each coloured by the symbol at its start: yellow while the run runs (`▶`), orange while it is paused (`⏸`), grey while it waits (`⧗`), green or red for 30 seconds after it ended (`✓`, `✗`):

```toml
[{ token = "$heavy", rules = [{ starts_with = "▶", fg = "#fabd2f", bold = true }, { starts_with = "⏸", fg = "#fe8019" }, { starts_with = "⧗", fg = "#928374" }, { starts_with = "✓", fg = "#b8bb26" }, { starts_with = "✗", fg = "#fb4934", bold = true }] }],
[{ token = "$heavy_2", rules = [{ starts_with = "▶", fg = "#fabd2f", bold = true }, { starts_with = "⏸", fg = "#fe8019" }, { starts_with = "⧗", fg = "#928374" }, { starts_with = "✓", fg = "#b8bb26" }, { starts_with = "✗", fg = "#fb4934", bold = true }] }],
[{ token = "$heavy_3", rules = [{ starts_with = "▶", fg = "#fabd2f", bold = true }, { starts_with = "⏸", fg = "#fe8019" }, { starts_with = "⧗", fg = "#928374" }, { starts_with = "✓", fg = "#b8bb26" }, { starts_with = "✗", fg = "#fb4934", bold = true }] }],
```

Read `~/.config/herdr/config.toml`. When it is a symlink, edit its target and name that path to the user. The rows go into every agent row list:

- `rows` under `[ui.sidebar.agents]`
- each list under `[ui.sidebar.agents.rows_by_agent]`, because an agent with its own list ignores `rows`

In each list, replace the row that already holds `$heavy` (heavy 0.6 added one) with the three rows; otherwise add them as the last rows, or right before a closing separator row such as `[{ token = "$sep" }]`. Without any `rows`, herdr uses its default `[["state_icon", "machine", "workspace", "tab"], ["agent"]]` (herdr config reference, `ui.sidebar.agents.rows`); write that default plus the three rows, so the rest of the sidebar looks as before. herdr allows at most 16 rows per list; if the three would pass that, say so and ask which rows to drop.

## 4. Bind the keys

The plugin's action `forward` moves the waiting run of the focused pane one place ahead; the action `dashboard` opens `heavy --dashboard` as a split pane next to the focused one. Add these blocks to the same config file, in the same diff as the rows, unless a `[[keys.command]]` with the same `command` exists already. When another binding uses `prefix+f` or `prefix+d`, pick a free key yourself and name it in the same question.

```toml
[[keys.command]]
key = "prefix+f"
type = "plugin_action"
command = "local.heavy-queue.forward"
description = "heavy: move the queued run of this pane one place ahead"

[[keys.command]]
key = "prefix+d"
type = "plugin_action"
command = "local.heavy-queue.dashboard"
description = "heavy: open the dashboard next to this pane"
```

The config is theirs and may hold changes of their own, so write only after a yes. Ask one yes/no question in everyday words and in the user's language, such as "I'll add three rows for the heavy runs and the keys prefix+f and prefix+d to your herdr config. OK?"; no diff and no lettered choices, the diff only when the user asks for it. Then run `herdr config check`, and only when it reports no errors, `herdr server reload-config`.

## 5. Check

The check must never hold up runs of other sessions: a test run that waits in the queue keeps them waiting behind it.

1. Run `herdr plugin action invoke refresh --plugin local.heavy-queue`, then `heavy --status --json`.
2. When a run waits or runs, check that run instead of starting one: `herdr agent list` shows its agent with a `tokens.heavy` such as `▶ 6m · ci`, and the sidebar shows it in yellow. Report that line and stop.
3. Only when no run waits or runs, run `heavy sleep 20` with `run_in_background` and look for this pane's agent (`$HERDR_PANE_ID`) in `herdr agent list`: its `tokens.heavy` reads `▶ <1m · sleep` in yellow, and after the 20 seconds `✓ <1m · sleep` in green, for 30 seconds. If another run starts to wait behind it, stop it with `TaskStop`. Report both observations in one line each.
