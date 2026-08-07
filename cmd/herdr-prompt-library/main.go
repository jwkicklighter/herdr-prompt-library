package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"herdr-prompt-library/internal/config"
	"herdr-prompt-library/internal/herdr"
	"herdr-prompt-library/internal/ui"
)

var errUsage = errors.New("usage: herdr-prompt-library <open|picker>")

func command(args []string) error {
	if len(args) != 1 {
		return errUsage
	}

	switch args[0] {
	case "open":
		return open(os.Getenv, herdr.Client{
			Binary: os.Getenv("HERDR_BIN_PATH"),
		})
	case "picker":
		return picker(os.Getenv, config.Load, config.ConfiguredLibraries, herdr.Client{Binary: os.Getenv("HERDR_BIN_PATH")})
	default:
		return fmt.Errorf("unknown command %q: %w", args[0], errUsage)
	}
}

func picker(getenv func(string) string, load func() ([]config.Prompt, error), configuredLibraries func() (config.Libraries, error), client herdr.Client) error {
	model, err := pickerModel(getenv, load, configuredLibraries, client)
	if err != nil {
		return err
	}
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("run prompt picker: %w", err)
	}
	return nil
}

func pickerModel(getenv func(string) string, load func() ([]config.Prompt, error), configuredLibraries func() (config.Libraries, error), client herdr.Client) (ui.Model, error) {
	targetPaneID := getenv(herdr.TargetPaneIDEnv)
	if targetPaneID == "" {
		return ui.Model{}, errors.New("open prompt picker: missing HERDR_PROMPT_LIBRARY_TARGET_PANE_ID")
	}
	libraries, err := configuredLibraries()
	if err != nil {
		return ui.Model{}, fmt.Errorf("configure prompt libraries: %w", err)
	}
	prompts, loadErr := load()
	return ui.NewWithInsertionAndLibraries(prompts, func(prompt config.Prompt) error {
		return client.SendText(targetPaneID, prompt.Contents)
	}, libraries, loadErr), nil
}

type pluginContext struct {
	WorkspaceCWD   string `json:"workspace_cwd"`
	FocusedPaneID  string `json:"focused_pane_id"`
	FocusedPaneCWD string `json:"focused_pane_cwd"`
	Worktree       struct {
		CheckoutPath string `json:"checkout_path"`
	} `json:"worktree"`
}

func open(getenv func(string) string, client herdr.Client) error {
	contextJSON := getenv("HERDR_PLUGIN_CONTEXT_JSON")
	if contextJSON == "" {
		return errors.New("open prompt library: missing HERDR_PLUGIN_CONTEXT_JSON")
	}

	var context pluginContext
	if err := json.Unmarshal([]byte(contextJSON), &context); err != nil {
		return fmt.Errorf("open prompt library: parse HERDR_PLUGIN_CONTEXT_JSON: %w", err)
	}
	if context.FocusedPaneID == "" {
		return errors.New("open prompt library: no focused pane in plugin context")
	}

	projectRoot := context.Worktree.CheckoutPath
	if projectRoot == "" {
		projectRoot = context.WorkspaceCWD
	}
	if projectRoot == "" {
		projectRoot = context.FocusedPaneCWD
	}
	if projectRoot == "" {
		return errors.New("open prompt library: no project root in plugin context")
	}

	return client.OpenPicker(context.FocusedPaneID, projectRoot, contextJSON)
}

func main() {
	if err := command(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
