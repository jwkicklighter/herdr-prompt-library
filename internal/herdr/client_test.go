package herdr

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestOpenPickerBuildsArgumentVector(t *testing.T) {
	var gotName string
	var gotArgs, gotEnv []string
	client := Client{
		Binary: "/tmp/herdr path/herdr",
		Run: func(name string, args []string, env []string) error {
			gotName = name
			gotArgs = args
			gotEnv = env
			return nil
		},
	}

	target := "pane; $(not-a-command)"
	root := "/tmp/project with spaces; $HOME"
	if err := client.OpenPicker(target, root); err != nil {
		t.Fatalf("OpenPicker() error = %v", err)
	}

	if gotName != client.Binary {
		t.Errorf("command name = %q, want %q", gotName, client.Binary)
	}
	wantArgs := []string{
		"plugin", "pane", "open",
		"--plugin", PluginID,
		"--entrypoint", PickerEntrypoint,
		"--env", TargetPaneIDEnv + "=" + target,
		"--env", ProjectRootEnv + "=" + root,
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Errorf("arguments = %#v, want %#v", gotArgs, wantArgs)
	}
	if gotEnv != nil {
		t.Errorf("runner environment = %#v, want nil", gotEnv)
	}
	for _, argument := range gotArgs {
		if argument == "--target-pane" {
			t.Error("popup open must target the active pane implicitly")
		}
	}
}

func TestOpenPickerReturnsRunnerError(t *testing.T) {
	want := errors.New("herdr unavailable")
	client := Client{Run: func(string, []string, []string) error { return want }}
	err := client.OpenPicker("pane-1", "/project")
	if !errors.Is(err, want) {
		t.Errorf("OpenPicker() error = %v, want %v", err, want)
	}
	if !strings.Contains(err.Error(), "open prompt picker") {
		t.Errorf("OpenPicker() error = %q, want actionable operation context", err)
	}
}

func TestSendTextPreservesExactArgumentWithoutSubmitting(t *testing.T) {
	var gotName string
	var gotArgs, gotEnv []string
	client := Client{
		Binary: "/tmp/herdr path/herdr",
		Run: func(name string, args []string, env []string) error {
			gotName = name
			gotArgs = args
			gotEnv = env
			return nil
		},
	}

	text := "first line\nsecond line  \n$HOME; $(not-a-command) & 'quoted'\t "
	if err := client.SendText("pane; $(not-a-command)", text); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}
	if gotName != client.Binary {
		t.Errorf("command name = %q, want %q", gotName, client.Binary)
	}
	wantArgs := []string{"pane", "send-text", "pane; $(not-a-command)", text}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Errorf("arguments = %#v, want %#v", gotArgs, wantArgs)
	}
	if gotEnv != nil {
		t.Errorf("runner environment = %#v, want nil", gotEnv)
	}
	for _, argument := range gotArgs {
		if argument == "send-keys" || argument == "enter" {
			t.Errorf("unexpected submitting command argument %q", argument)
		}
	}
}

func TestSendTextUsesDefaultBinaryAndReturnsRunnerError(t *testing.T) {
	want := errors.New("permission denied")
	var gotName string
	client := Client{Run: func(name string, _ []string, _ []string) error {
		gotName = name
		return want
	}}
	if err := client.SendText("pane-1", "text"); !errors.Is(err, want) {
		t.Errorf("SendText() error = %v, want wrapped %v", err, want)
	}
	if gotName != DefaultBinary {
		t.Errorf("command name = %q, want default %q", gotName, DefaultBinary)
	}
}
