// Package config loads prompt definitions from Herdr configuration files.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	// SourceProject identifies prompts declared by the current project.
	SourceProject = "project"
	// SourceGlobal identifies prompts declared in the user's plugin config.
	SourceGlobal = "global"
)

// Prompt is a prompt definition ready for display and insertion.
type Prompt struct {
	Name        string
	Description string
	Contents    string
	Source      string
}

type promptFile struct {
	Prompts []promptDefinition `toml:"prompts"`
}

type promptDefinition struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Contents    string `toml:"contents"`
}

// Load reads global prompts from $HERDR_PLUGIN_CONFIG_DIR/prompts.toml and
// project prompts from .herdr/prompts.toml below HERDR_PROMPT_LIBRARY_PROJECT_ROOT.
// When that variable is absent, the current working directory is the project root.
func Load() ([]Prompt, error) {
	projectRoot := os.Getenv("HERDR_PROMPT_LIBRARY_PROJECT_ROOT")
	if projectRoot == "" {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("determine current project directory: %w", err)
		}
		projectRoot = workingDirectory
	}

	globalPath := ""
	if configDirectory := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); configDirectory != "" {
		globalPath = filepath.Join(configDirectory, "prompts.toml")
	}

	return LoadFiles(globalPath, filepath.Join(projectRoot, ".herdr", "prompts.toml"))
}

// LoadFiles reads project and global prompt files. Empty and missing paths are
// empty scopes. Project prompts are returned before global prompts.
func LoadFiles(globalPath, projectPath string) ([]Prompt, error) {
	global, err := loadFile(globalPath, SourceGlobal)
	if err != nil {
		return nil, err
	}
	project, err := loadFile(projectPath, SourceProject)
	if err != nil {
		return nil, err
	}

	return append(project, global...), nil
}

func loadFile(path, source string) ([]Prompt, error) {
	if path == "" {
		return nil, nil
	}

	var file promptFile
	if _, err := toml.DecodeFile(path, &file); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("load %s prompts from %q: %w", source, path, err)
	}

	prompts := make([]Prompt, len(file.Prompts))
	for index, definition := range file.Prompts {
		if err := validate(definition, path, index+1); err != nil {
			return nil, err
		}
		prompts[index] = Prompt{
			Name:        definition.Name,
			Description: definition.Description,
			Contents:    definition.Contents,
			Source:      source,
		}
	}

	return prompts, nil
}

func validate(prompt promptDefinition, path string, number int) error {
	for field, value := range map[string]string{
		"name":        prompt.Name,
		"description": prompt.Description,
		"contents":    prompt.Contents,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("invalid prompt %d in %q: %s must not be blank", number, path, field)
		}
	}
	return nil
}
