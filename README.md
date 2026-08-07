# Herdr Prompt Library

Prompt Library is a local [Herdr](https://herdr.dev) plugin for browsing, managing, and inserting prompts into the pane that was focused when the picker opened. Prompts are Markdown files. Insertion is literal: frontmatter is metadata and only the Markdown body is sent; shell syntax is not executed and the prompt is not submitted.

## Requirements

- Herdr 0.7.5 or later
- Go 1.25 or later, available as `go`

## Build and link

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

Both directories are optional. Prompt Library discovers Markdown files recursively below each root, so subdirectories can organize a library. Only files ending in `.md` are prompt files. Local prompts are shown before global prompts and retain their `LOCAL` or `GLOBAL` source label.

Create the global directory when adding the first global prompt:

```sh
mkdir -p "$(herdr plugin config-dir herdr.prompt-library)/prompts"
```

Copy and adapt the individual examples in [`examples/prompts/`](examples/prompts/) into either directory. Each file has YAML frontmatter with a required, nonblank `title` and an optional `description`, followed by a required, nonblank literal prompt body. The description may be omitted or blank:

```markdown
---
title: Explain code
description: Explain the selected code concisely.
---

Explain this code concisely, including its inputs and outputs.
```

The closing frontmatter delimiter must be on its own line. Everything after it is the body and is inserted exactly, including blank lines, indentation, trailing spaces, and final newlines. Frontmatter is never inserted. A filename is an organizational detail, not the prompt title; titles do not need to be unique.

## Views and search

The picker has three source views:

- `All`: local and global prompts
- `Local`: prompts below the focused project's `.herdr/prompts/`
- `Global`: prompts below the plugin config `prompts/`

The picker provides `All`, `Local`, and `Global` views; the default is `All`. Switching views never changes files. Each entry shows its title, optional description, and `LOCAL` or `GLOBAL` source badge. Duplicate titles remain separate entries.

Ordinary typing always enters the fuzzy search. Search matches the title, description, and body. Matching is case-insensitive; clearing the query restores the active view. Search never edits prompt contents. `Enter` immediately inserts the selected result; it does not open a separate selection step.

## Managing prompts

Management actions open in-popup forms or confirmations. They operate on the selected entry and use the active source/location unless stated otherwise:

- `Alt+A`: create a prompt
- `Alt+E`: edit the selected prompt
- `Alt+D`: delete the selected prompt after an in-popup confirmation
- `Alt+U`: duplicate the selected prompt in a prefilled form
- `Alt+M`: move the selected prompt directly to the opposite scope after an in-popup confirmation

Create and duplicate forms contain title, description, multiline body, and destination fields. Move has no destination picker: it goes directly from local to global or global to local. Filenames are generated from the title as slugs; when a slug already exists, a collision suffix is generated rather than replacing the existing file. Editing preserves the selected prompt's filename. Delete and move never affect a different prompt with the same title.

`Ctrl+S` saves the active form. `Tab` moves between form fields and `Esc` cancels the form or confirmation without inserting or changing files. Saved changes are picked up the next time the library is refreshed or reopened. Insertion sends only the selected Markdown body to the original pane and does not modify the prompt file.

## Discovery and errors

Missing global or local directories are treated as empty libraries. The picker does not create either directory merely by opening it; write actions create the selected root when needed. Permission errors, unreadable files, malformed YAML frontmatter, missing required fields, and invalid frontmatter types are reported with the affected path. A bad file is skipped so valid prompts remain usable, and no write action is performed on a file that could not be parsed.

Malformed frontmatter never causes the body to be treated as a prompt, and a missing closing delimiter is an error. Read-only directories or files make the relevant create/edit/delete/duplicate/move action fail without partial replacement. Writes use a temporary file and replace the target only after a successful save, so an interrupted or failed operation does not truncate the original.

Prompt titles are display metadata, not filenames or keys. Two files may have the same title, including one local and one global prompt. Files with the same basename in different directories are distinct; files that resolve to the same destination path are a collision and are never silently replaced.

## Controls

- `up`/`down` or `j`/`k`: move through prompts
- `pgup`/`pgdown` or `ctrl+u`/`ctrl+d`: scroll the preview
- `home`/`end`: jump to the top or bottom of the preview
- ordinary typing: fuzzy-search titles, descriptions, and bodies
- `enter`: immediately insert the selected prompt into the pane that was focused before the popup opened
- `Alt+A`: create form
- `Alt+E`: edit form
- `Alt+D`: delete confirmation
- `Alt+U`: duplicate form
- `Alt+M`: move confirmation to the opposite scope
- `Tab`: move to the next form field
- `Ctrl+S`: save the active form
- `esc`: cancel the active form, confirmation, or picker without inserting

The popup resizes with Herdr. On wide terminals, the prompt list and preview are side by side; on narrower terminals, they stack vertically. Closing the popup returns focus to the pane that opened it.

## Troubleshooting

- **The keybinding does nothing:** confirm the plugin is linked and enabled with `herdr plugin list --plugin herdr.prompt-library`, then run `herdr server reload-config` after adding or changing the `[[keys.command]]` entry.
- **No prompts appear:** verify the local or global `prompts/` directory contains recursive `.md` files. The global root is based on `herdr plugin config-dir herdr.prompt-library`.
- **A file is skipped:** inspect the path-specific error for malformed frontmatter, a missing or blank `title` or body, an unsupported field type, or a permissions problem. Descriptions may be omitted or blank.
- **Create or duplicate refuses to save:** ensure the destination directory is writable. Filenames are generated from the title and receive a collision suffix automatically.
- **Move refuses to save:** ensure the opposite-scope directory is writable; move always targets the opposite scope.
- **A changed prompt is not visible:** close and reopen the picker. Prompt files are loaded on open.
- **Insertion fails:** ensure Herdr is still running and that the pane that opened the picker still exists. The picker remains open so you can retry after fixing the problem.
- **A different prompt was inserted:** duplicate titles are separate entries. Use the source badge, description, and preview to select the intended entry.
