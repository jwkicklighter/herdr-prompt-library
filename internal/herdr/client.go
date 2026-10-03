// Package herdr invokes the Herdr CLI without involving a shell.
package herdr

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	PluginID         = "herdr.prompt-library"
	PickerEntrypoint = "picker"
	TabIDEnv         = "HERDR_TAB_ID"
	PluginContextEnv = "HERDR_PLUGIN_CONTEXT_JSON"
	TargetPaneIDEnv  = "HERDR_PROMPT_LIBRARY_TARGET_PANE_ID"
	ProjectRootEnv   = "HERDR_PROMPT_LIBRARY_PROJECT_ROOT"
	DirectoryEnv     = "HERDR_PROMPT_LIBRARY_DIRECTORY"
	AgentPrompt      = "agent prompt"
	SendText         = "pane send-text"
	DefaultBinary    = "herdr"
)

// Runner executes a command by path and discrete arguments. It is injectable
// so callers can test the exact command without executing Herdr.
type Runner func(name string, args []string, env []string) error

// Client invokes the Herdr CLI for plugin pane operations.
type Client struct {
	Binary        string
	InsertCommand string
	Run           Runner
}

// InsertPrompt sends text to targetPaneID using the configured Herdr command.
// The text is passed as one argv element, preserving whitespace and shell metacharacters.
func (c Client) InsertPrompt(targetPaneID, text string) error {
	binary := c.Binary
	if binary == "" {
		binary = DefaultBinary
	}
	run := c.Run
	if run == nil {
		run = runCommand
	}

	command := c.InsertCommand
	if command == "" {
		command = SendText
	}
	var args []string
	switch command {
	case AgentPrompt:
		args = []string{"agent", "prompt", targetPaneID, text}
	case SendText:
		args = []string{"pane", "send-text", targetPaneID, text}
	default:
		return fmt.Errorf("insert prompt into pane %q: unsupported command %q (choose %q or %q)", targetPaneID, command, AgentPrompt, SendText)
	}

	if err := run(binary, args, nil); err != nil {
		return fmt.Errorf("insert prompt into pane %q: %w", targetPaneID, err)
	}
	return nil
}

// OpenPicker opens the manifest-defined picker against targetPaneID. Context
// captured by the action is placed in the picker environment for later use.
func (c Client) OpenPicker(targetPaneID, projectRoot, tabID, pluginContextJSON, directory string) error {
	binary := c.Binary
	if binary == "" {
		binary = DefaultBinary
	}
	run := c.Run
	if run == nil {
		run = runCommand
	}

	args := []string{
		"plugin", "pane", "open",
		"--plugin", PluginID,
		"--entrypoint", PickerEntrypoint,
		"--env", TargetPaneIDEnv + "=" + targetPaneID,
		"--env", ProjectRootEnv + "=" + projectRoot,
		"--env", TabIDEnv + "=" + tabID,
		"--env", PluginContextEnv + "=" + pluginContextJSON,
		"--env", DirectoryEnv + "=" + directory,
	}
	if err := run(binary, args, nil); err != nil {
		return fmt.Errorf("open prompt picker: %w", err)
	}
	return nil
}

func runCommand(name string, args []string, env []string) error {
	command := exec.Command(name, args...)
	if len(env) > 0 {
		command.Env = append(os.Environ(), env...)
	}
	output, err := command.CombinedOutput()
	if err == nil || len(output) == 0 {
		return err
	}
	return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
}
