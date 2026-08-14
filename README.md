# Herdr Prompt Library

Prompt Library is a [Herdr](https://herdr.dev) plugin for browsing, managing, and inserting prompts into the pane that was focused when the picker opened. Prompts are Markdown files. Insertion is literal: frontmatter is metadata and only the Markdown body is sent; shell syntax is not executed and the prompt is not submitted.

## Requirements

- Herdr 0.7.5 or later
- Go 1.25 or later, available as `go`

## Installation

Install the plugin directly from this Git repository with Herdr:

```sh
herdr plugin install jwkicklighter/herdr-prompt-library
```

## Build and link locally

From this repository, build the plugin:

```sh
make build
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

Run `herdr plugin list --plugin herdr.prompt-library` to inspect the linked plugin. If you edit Go code, rebuild before using the updated binary. Prompt files are read whenever the picker opens, so close and reopen the picker after editing a prompt file; no Herdr config reload is needed for prompt content changes.

Plugin-action keybindings use a globally qualified action ID: the manifest plugin ID, a period, and the local manifest action ID. To invoke the same action directly from an active Herdr pane:

```sh
herdr plugin action invoke herdr.prompt-library.open
```

`herdr.prompt-library.open` is the registered action identifier. `herdr.prompt-library:open` is not registered.

## Prompt files

Prompt Library combines two optional prompt directories:

- Global prompts: `$(herdr plugin config-dir herdr.prompt-library)/prompts/`
- Local prompts: `.herdr/prompts/` in the project root of the focused pane

Both directories are optional. Prompt Library discovers Markdown files recursively below each root, so subdirectories can organize a library. Files ending in `.md`, matched case-insensitively, are prompt files. Local prompts are shown before global prompts and retain their `LOCAL` or `GLOBAL` source label.

Create the global directory when adding the first global prompt:

```sh
mkdir -p "$(herdr plugin config-dir herdr.prompt-library)/prompts"
```

Copy and adapt the individual examples in [`examples/prompts/`](examples/prompts/) into either directory. Each file has YAML frontmatter with a required, nonblank `title`, followed by a required, nonblank literal prompt body. Existing `description` and other frontmatter fields are preserved but are not shown or searched:

```markdown
---
title: Explain code
---

Explain this code concisely, including its inputs and outputs.
```

The closing frontmatter delimiter must be on its own line. Everything after it is the body and is inserted exactly, including blank lines, indentation, trailing spaces, and final newlines. Frontmatter is never inserted. A filename is an organizational detail, not the prompt title; titles do not need to be unique.

## Herdr context and placeholders

Prompt bodies support these five lowercase placeholders when they are inserted:

| Placeholder | Value |
| --- | --- |
| `herdr_tab_id` | The Herdr tab ID captured when the picker was activated. |
| `herdr_plugin_context_json` | The raw `HERDR_PLUGIN_CONTEXT_JSON` value captured when the picker was activated. It is inserted as JSON text without parsing or reformatting. |
| `today` | The local insertion-time date in `YYYY-MM-DD` format. |
| `now` | The local insertion-time datetime in RFC 3339 format, including the local timezone offset (for example, `2026-08-13T09:15:00-04:00`). |
| `directory` | The focused pane's current working directory, captured when the picker was activated. This is the pane CWD, not a project-root fallback. |

Names are matched case-sensitively, and any amount of ASCII whitespace is allowed inside the braces. Both compact and spaced syntax work:

```text
Date: {{today}}
Directory: {{ directory }}
Context: {{      herdr_plugin_context_json      }}
```

Expansion happens only immediately before insertion. Prompt files are not rewritten, so saved tokens remain in the file exactly as written. Unknown names, differently cased names, malformed braces, and other token-like text remain literal and are inserted unchanged.

## Views and search

The picker has three source views:

- `All`: local and global prompts
- `Local`: prompts below the focused project's `.herdr/prompts/`
- `Global`: prompts below the plugin config `prompts/`

The picker provides `All`, `Local`, and `Global` views; the default is `All`. Switching views never changes files. Each entry shows its title, two wrapped lines from the beginning of its prompt, and a `LOCAL` or `GLOBAL` source badge. Duplicate titles remain separate entries.

Search starts unfocused so navigation and action shortcuts are immediately available. Press `/` to focus it and show the text cursor. Search matches the title and prompt body, not frontmatter descriptions, and is case-insensitive. While search is focused, `Esc` clears the query and returns focus to the picker; `Enter` or `Tab` keeps the query and returns focus without inserting a prompt or changing scope. With search unfocused, `Enter` inserts the selected result.

## Managing prompts

Management actions open in-popup forms or confirmations. They operate on the selected entry and use the active source/location unless stated otherwise:

- `a`: create a prompt
- `e`: edit the selected prompt
- `Alt+D`: delete the selected prompt after an in-popup confirmation
- `d`: duplicate the selected prompt in a prefilled form, followed by a final confirmation
- `m`: move the selected prompt directly to the opposite scope after an in-popup confirmation

Create and duplicate forms contain title, multiline prompt, and a Local/Global destination control. Edit forms contain only title and prompt. Duplicate shows the source title, proposed copy title, and destination for confirmation before writing; a failed write keeps the entered values available for correction and retry. Move has no destination picker: its label and confirmation identify the opposite scope, local to global or global to local. Filenames are generated from the title as slugs; when a slug already exists, a collision suffix is generated rather than replacing the existing file. Editing preserves the selected prompt's filename and existing frontmatter metadata. Delete and move never affect a different prompt with the same title.

`Ctrl+S` saves the active form. `Tab` moves between form fields and `Esc` cancels the form or confirmation without inserting or changing files. Saved changes are picked up the next time the library is refreshed or reopened. Insertion sends only the selected Markdown body to the original pane and does not modify the prompt file.

## Discovery and errors

Missing global or local directories are treated as empty libraries. The picker does not create either directory merely by opening it; write actions create the selected root when needed. Permission errors, unreadable files, malformed YAML frontmatter, missing required fields, and invalid frontmatter types are reported with the affected path. A bad file is skipped so valid prompts remain usable, and no write action is performed on a file that could not be parsed.

Malformed frontmatter never causes the body to be treated as a prompt, and a missing closing delimiter is an error. Read-only directories or files make the relevant create/edit/delete/duplicate/move action fail without partial replacement. Writes use a temporary file and replace the target only after a successful save, so an interrupted or failed operation does not truncate the original.

Prompt titles are display metadata, not filenames or keys. Two files may have the same title, including one local and one global prompt. Files with the same basename in different directories are distinct; files that resolve to the same destination path are a collision and are never silently replaced.

## Controls

- `up`/`down` or `j`/`k`: move through prompts
- `pgup`/`pgdown` or `ctrl+u`/`ctrl+d`: scroll the preview
- `home`/`end`: jump to the top or bottom of the preview
- `/`: focus fuzzy search; type to filter titles and prompt bodies
- `enter`/`Tab` while searching: keep the query and return focus to picker controls
- `esc` while searching: clear the query and return focus to picker controls
- `enter` while search is unfocused: insert the selected prompt into the pane that was focused before the popup opened
- `a`: create form
- `e`: edit form
- `Alt+D`: delete confirmation
- `d`: duplicate form and final confirmation
- `m`: move confirmation to the opposite scope
- `?`: show or close the context-aware keyboard shortcut dialog outside text entry
- `Tab`: move to the next form field
- `Ctrl+S`: save the active form
- `enter`/`y`: accept a confirmation
- `esc`/`n`: cancel a form or confirmation; `esc` closes the unfocused picker without inserting

The popup resizes with Herdr. On wide terminals, the prompt list and preview are side by side; on narrower terminals, they stack vertically. Closing the popup returns focus to the pane that opened it.

Picker accents use ANSI cyan and muted text uses ANSI gray. Herdr forwards its active terminal palette to pane applications, so these colors follow the active Herdr/terminal scheme without a plugin-specific theme setting.

## Troubleshooting

- **The keybinding does nothing:** confirm the plugin is linked and enabled with `herdr plugin list --plugin herdr.prompt-library`, then run `herdr server reload-config` after adding or changing the `[[keys.command]]` entry.
- **No prompts appear:** verify the local or global `prompts/` directory contains recursive `.md` files. The global root is based on `herdr plugin config-dir herdr.prompt-library`.
- **A file is skipped:** inspect the path-specific error for malformed frontmatter, a missing or blank `title` or body, an unsupported field type, or a permissions problem.
- **Create or duplicate refuses to save:** ensure the destination directory is writable. Filenames are generated from the title and receive a collision suffix automatically.
- **Move refuses to save:** ensure the opposite-scope directory is writable; move always targets the opposite scope.
- **A changed prompt is not visible:** close and reopen the picker. Prompt files are loaded on open.
- **Insertion fails:** ensure Herdr is still running and that the pane that opened the picker still exists. The picker remains open so you can retry after fixing the problem.
- **A different prompt was inserted:** duplicate titles are separate entries. Use the source badge, prompt excerpt, and preview to select the intended entry.

## AI disclosure

This project was developed with assistance from AI coding tools. AI-assisted changes are reviewed and tested by the maintainer before inclusion.
