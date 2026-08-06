# Herdr Prompt Library

Prompt Library is a local [Herdr](https://herdr.dev) plugin for browsing named prompts and inserting the selected prompt into the pane that was focused when the picker opened. Insertion is literal: it does not execute shell syntax and does not submit the prompt.

## Requirements

- Herdr 0.7.5 or later
- Go 1.25 or later, available as `go`

## Build and link

From this repository, build the plugin:

```sh
go build -o bin/herdr-prompt-library ./cmd/herdr-prompt-library
```

Link the local checkout and enable it:

```sh
herdr plugin link . --enabled
```

Add a keybinding to your Herdr config (`~/.config/herdr/config.toml` by default):

```toml
[[keys.command]]
key = "prefix+a"
type = "plugin_action"
command = "herdr.prompt-library.open"
```

`prefix+a` is recommended because Herdr uses `prefix+p` for the previous tab by default. Reload the running Herdr configuration after changing the binding:

```sh
herdr server reload-config
```

To remove the local plugin link later:

```sh
herdr plugin unlink herdr.prompt-library
```

Run `herdr plugin list --plugin herdr.prompt-library` to inspect the linked plugin. If you edit Go code, rebuild before using the updated binary. Prompt files themselves are read whenever the picker opens, so close and reopen the picker after editing a prompt file; no Herdr config reload is needed for prompt content changes.

Plugin-action keybindings use a globally qualified action ID: the manifest plugin ID, a period, and the local manifest action ID. To invoke the same action directly from an active Herdr pane:

```sh
herdr plugin action invoke herdr.prompt-library.open
```

`herdr.prompt-library.open` is the registered action identifier. `herdr.prompt-library:open` is not registered.

## Prompt files

Prompt Library combines two optional files:

- Global prompts: `$(herdr plugin config-dir herdr.prompt-library)/prompts.toml`
- Project prompts: `.herdr/prompts.toml` in the project root of the focused pane

Create the global directory if it does not already exist:

```sh
mkdir -p "$(herdr plugin config-dir herdr.prompt-library)"
```

Copy and adapt [`examples/prompts.toml`](examples/prompts.toml) to either location. Both files use TOML arrays of tables. Every prompt must have nonblank `name`, `description`, and `contents` fields:

```toml
[[prompts]]
name = "Explain code"
description = "Explain the selected code concisely."
contents = "Explain this code concisely, including its inputs and outputs."

[[prompts]]
name = "Review change"
description = "Review the current change for correctness."
contents = '''Review this change for correctness and regressions.

List findings by severity with file and line references.'''
```

Use TOML basic strings (`"..."`) for single-line prompts and literal multiline strings (`'''...'''`) for multiline prompts. The selected `contents` value is inserted exactly, including newlines and whitespace.

Project prompts appear before global prompts. Prompt names are not keys: duplicate names are intentionally retained, shown with `PROJECT` or `GLOBAL` badges, and each entry inserts its own contents. This lets a project-specific prompt coexist with a global prompt of the same name.

## Controls

- `up`/`down` or `j`/`k`: move through prompts
- `pgup`/`pgdown` or `ctrl+u`/`ctrl+d`: scroll the preview
- `home`/`end`: jump to the top or bottom of the preview
- `enter`: insert the selected prompt into the pane that was focused before the popup opened
- `esc` or `q`: cancel without inserting

The popup resizes with Herdr. On wide terminals, the prompt list and preview are side by side; on narrower terminals, they stack vertically. Closing the popup returns focus to the pane that opened it.

## Troubleshooting

- **The keybinding does nothing:** confirm the plugin is linked and enabled with `herdr plugin list --plugin herdr.prompt-library`, then run `herdr server reload-config` after adding or changing the `[[keys.command]]` entry.
- **No prompts appear:** verify one of the two paths above exists and contains `[[prompts]]` entries. The global path is printed by `herdr plugin config-dir herdr.prompt-library`.
- **The picker reports a TOML error:** correct the named file and reopen the picker. All three fields are required and must not be blank.
- **A changed prompt is not visible:** close and reopen the picker. Prompt files are loaded on open.
- **Insertion fails:** ensure Herdr is still running and that the pane that opened the picker still exists. The picker remains open so you can retry after fixing the problem.
- **A different prompt was inserted:** duplicate names are separate entries. Use the `PROJECT` or `GLOBAL` badge and description to select the intended entry.
