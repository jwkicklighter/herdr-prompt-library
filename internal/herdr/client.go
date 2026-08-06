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
	TargetPaneIDEnv  = "HERDR_PROMPT_LIBRARY_TARGET_PANE_ID"
	ProjectRootEnv   = "HERDR_PROMPT_LIBRARY_PROJECT_ROOT"
	DefaultBinary    = "herdr"
)

// Runner executes a command by path and discrete arguments. It is injectable
// so callers can test the exact command without executing Herdr.
type Runner func(name string, args []string, env []string) error

// Client invokes the Herdr CLI for plugin pane operations.
type Client struct {
	Binary string
	Run    Runner
}

// SendText inserts text into targetPaneID without submitting it. The text is
// passed as one argv element, preserving whitespace and shell metacharacters.
func (c Client) SendText(targetPaneID, text string) error {
	binary := c.Binary
	if binary == "" {
		binary = DefaultBinary
	}
	run := c.Run
	if run == nil {
		run = runCommand
	}

	if err := run(binary, []string{"pane", "send-text", targetPaneID, text}, nil); err != nil {
		return fmt.Errorf("insert prompt into pane %q: %w", targetPaneID, err)
	}
	return nil
}

// OpenPicker opens the manifest-defined picker against targetPaneID. The
// target and root are also placed in the picker environment for later actions.
func (c Client) OpenPicker(targetPaneID, projectRoot string) error {
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
