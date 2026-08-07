// Package config loads prompt definitions from Markdown files.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var walkDirectory = filepath.WalkDir

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
	Path        string
}

type promptDefinition struct {
	Name        string `yaml:"title"`
	Description string `yaml:"description"`
}

// Load reads global prompts from $HERDR_PLUGIN_CONFIG_DIR/prompts and project
// prompts from .herdr/prompts below HERDR_PROMPT_LIBRARY_PROJECT_ROOT.
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
		globalPath = filepath.Join(configDirectory, "prompts")
	}

	return LoadFiles(globalPath, filepath.Join(projectRoot, ".herdr", "prompts"))
}

// LoadFiles reads project and global prompt directories. Empty and missing paths are
// empty scopes. Project prompts are returned before global prompts.
func LoadFiles(globalPath, projectPath string) ([]Prompt, error) {
	global, globalErr := loadDirectory(globalPath, SourceGlobal)
	project, projectErr := loadDirectory(projectPath, SourceProject)
	return append(project, global...), errors.Join(projectErr, globalErr)
}

func loadDirectory(path, source string) ([]Prompt, error) {
	if path == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("load %s prompts from %q: %w", source, path, err)
	}

	var paths []string
	var errs []error
	if err := walkDirectory(path, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, fmt.Errorf("traverse %s prompts at %q: %w", source, filePath, err))
			return nil
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(filePath), ".md") {
			paths = append(paths, filePath)
		}
		return nil
	}); err != nil {
		errs = append(errs, fmt.Errorf("load %s prompts from %q: %w", source, path, err))
	}
	sort.Strings(paths)

	var prompts []Prompt
	for _, filePath := range paths {
		prompt, err := loadPrompt(filePath, source)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		prompts = append(prompts, prompt)
	}
	return prompts, errors.Join(errs...)
}

func loadPrompt(path, source string) (Prompt, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Prompt{}, fmt.Errorf("load %s prompt from %q: %w", source, path, err)
	}

	frontmatter, body, err := splitFrontmatter(contents)
	if err != nil {
		return Prompt{}, fmt.Errorf("invalid %s prompt in %q: %w", source, path, err)
	}

	var definition promptDefinition
	if err := yaml.Unmarshal(frontmatter, &definition); err != nil {
		return Prompt{}, fmt.Errorf("parse %s prompt frontmatter in %q: %w", source, path, err)
	}
	prompt := Prompt{
		Name:        definition.Name,
		Description: definition.Description,
		Contents:    string(body),
		Source:      source,
		Path:        path,
	}
	if err := validate(prompt); err != nil {
		return Prompt{}, fmt.Errorf("invalid %s prompt in %q: %w", source, path, err)
	}
	return prompt, nil
}

func splitFrontmatter(contents []byte) ([]byte, []byte, error) {
	frontmatter, body, _, err := splitFrontmatterStyle(contents)
	return frontmatter, body, err
}

func splitFrontmatterStyle(contents []byte) ([]byte, []byte, []byte, error) {
	for _, newline := range [][]byte{[]byte("\n"), []byte("\r\n")} {
		opening := append([]byte("---"), newline...)
		if !bytes.HasPrefix(contents, opening) {
			continue
		}
		frontmatter := contents[len(opening):]
		closing := append(append([]byte{}, newline...), []byte("---")...)
		if index := bytes.Index(frontmatter, append(closing, newline...)); index >= 0 {
			return frontmatter[:index], frontmatter[index+len(closing)+len(newline):], newline, nil
		}
		if bytes.HasSuffix(frontmatter, closing) {
			return frontmatter[:len(frontmatter)-len(closing)], nil, newline, nil
		}
		return nil, nil, nil, errors.New("missing YAML frontmatter closing delimiter")
	}
	return nil, nil, nil, errors.New("missing YAML frontmatter opening delimiter")
}

func validate(prompt Prompt) error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{"title", prompt.Name},
		{"description", prompt.Description},
		{"contents", prompt.Contents},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s must not be blank", field.name)
		}
	}
	return nil
}
