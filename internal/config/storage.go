package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Libraries identifies the local and global prompt directories.
type Libraries struct {
	LocalDir  string
	GlobalDir string
}

// PartialMoveError reports that a destination was created but its source could
// not be removed. The returned destination prompt can be used to clean up.
type PartialMoveError struct {
	SourcePath      string
	DestinationPath string
	Err             error
}

func (err *PartialMoveError) Error() string {
	return fmt.Sprintf("move prompt: created %q but could not remove %q: %v", err.DestinationPath, err.SourcePath, err.Err)
}

func (err *PartialMoveError) Unwrap() error { return err.Err }

var (
	createTempFile = os.CreateTemp
	linkFile       = os.Link
	renameFile     = os.Rename
	removeFile     = os.Remove
)

// ConfiguredLibraries returns the prompt directories configured for this
// process. A missing global configuration leaves GlobalDir empty.
func ConfiguredLibraries() (Libraries, error) {
	projectRoot := os.Getenv("HERDR_PROMPT_LIBRARY_PROJECT_ROOT")
	if projectRoot == "" {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return Libraries{}, fmt.Errorf("determine current project directory: %w", err)
		}
		projectRoot = workingDirectory
	}

	libraries := Libraries{LocalDir: filepath.Join(projectRoot, ".herdr", "prompts")}
	if configDirectory := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); configDirectory != "" {
		libraries.GlobalDir = filepath.Join(configDirectory, "prompts")
	}
	return libraries, nil
}

// Create writes prompt to source, creating its library directory when needed.
// It never overwrites an existing file and returns the collision-resolved path.
func (libraries Libraries) Create(source string, prompt Prompt) (Prompt, error) {
	if err := validate(prompt); err != nil {
		return Prompt{}, err
	}
	contents, err := encodePrompt(prompt.Name, prompt.Description, prompt.Contents)
	if err != nil {
		return Prompt{}, err
	}
	path, err := libraries.writeUnique(source, slug(prompt.Name), contents)
	if err != nil {
		return Prompt{}, err
	}
	prompt.Source = source
	prompt.Path = path
	return prompt, nil
}

// Update atomically replaces prompt's title, description, and body without
// changing its filename. Unrecognized YAML frontmatter fields are retained.
func (libraries Libraries) Update(prompt, changes Prompt) (Prompt, error) {
	path, err := libraries.safePromptPath(prompt)
	if err != nil {
		return Prompt{}, err
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return Prompt{}, fmt.Errorf("read prompt %q: %w", path, err)
	}
	frontmatter, _, newline, err := splitFrontmatterStyle(contents)
	if err != nil {
		return Prompt{}, fmt.Errorf("read prompt %q: %w", path, err)
	}

	var metadata yaml.Node
	if err := yaml.Unmarshal(frontmatter, &metadata); err != nil {
		return Prompt{}, fmt.Errorf("parse prompt frontmatter in %q: %w", path, err)
	}
	frontmatter, err = updateMetadata(frontmatter, newline, &metadata, changes.Name, changes.Description)
	if err != nil {
		return Prompt{}, fmt.Errorf("update prompt frontmatter in %q: %w", path, err)
	}
	updated := Prompt{Name: changes.Name, Description: changes.Description, Contents: changes.Contents, Source: prompt.Source, Path: prompt.Path}
	if err := validate(updated); err != nil {
		return Prompt{}, err
	}
	updatedContents := append([]byte("---"), newline...)
	updatedContents = append(updatedContents, frontmatter...)
	updatedContents = append(updatedContents, newline...)
	updatedContents = append(updatedContents, "---"...)
	updatedContents = append(updatedContents, newline...)
	updatedContents = append(updatedContents, updated.Contents...)
	if err := atomicReplace(path, updatedContents); err != nil {
		return Prompt{}, fmt.Errorf("update prompt %q: %w", path, err)
	}
	return updated, nil
}

// Delete removes prompt after confirming its path stays inside its source library.
func (libraries Libraries) Delete(prompt Prompt) error {
	path, err := libraries.safePromptPath(prompt)
	if err != nil {
		return err
	}
	if err := removeFile(path); err != nil {
		return fmt.Errorf("delete prompt %q: %w", path, err)
	}
	return nil
}

// Duplicate copies the complete Markdown file into destination, preserving all
// frontmatter metadata and the raw prompt body.
func (libraries Libraries) Duplicate(prompt Prompt, destination string) (Prompt, error) {
	path, err := libraries.safePromptPath(prompt)
	if err != nil {
		return Prompt{}, err
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return Prompt{}, fmt.Errorf("read prompt %q: %w", path, err)
	}
	destinationPath, err := libraries.writeUnique(destination, slug(prompt.Name), contents)
	if err != nil {
		return Prompt{}, err
	}
	prompt.Source = destination
	prompt.Path = destinationPath
	return prompt, nil
}

// Move atomically creates a collision-free copy in destination, then removes
// the source. A failed source removal returns the created destination together
// with a PartialMoveError.
func (libraries Libraries) Move(prompt Prompt, destination string) (Prompt, error) {
	if destination == prompt.Source {
		return Prompt{}, fmt.Errorf("move prompt: source and destination are both %s", destination)
	}
	path, err := libraries.safePromptPath(prompt)
	if err != nil {
		return Prompt{}, err
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return Prompt{}, fmt.Errorf("read prompt %q: %w", path, err)
	}
	destinationPath, err := libraries.writeUnique(destination, slug(prompt.Name), contents)
	if err != nil {
		return Prompt{}, err
	}
	moved := prompt
	moved.Source = destination
	moved.Path = destinationPath
	if err := removeFile(path); err != nil {
		return moved, &PartialMoveError{SourcePath: path, DestinationPath: destinationPath, Err: err}
	}
	return moved, nil
}

func (libraries Libraries) writeUnique(source, base string, contents []byte) (string, error) {
	root, err := libraries.ensureRoot(source)
	if err != nil {
		return "", err
	}
	for suffix := 1; ; suffix++ {
		name := base + ".md"
		if suffix > 1 {
			name = fmt.Sprintf("%s-%d.md", base, suffix)
		}
		path := filepath.Join(root, name)
		created, err := atomicCreate(path, contents)
		if errors.Is(err, fsErrExists) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create %s prompt %q: %w", source, path, err)
		}
		if created {
			return path, nil
		}
	}
}

func (libraries Libraries) ensureRoot(source string) (string, error) {
	root, err := libraries.root(source)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create %s prompt directory %q: %w", source, root, err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve %s prompt directory %q: %w", source, root, err)
	}
	return resolved, nil
}

func (libraries Libraries) safePromptPath(prompt Prompt) (string, error) {
	root, err := libraries.root(prompt.Source)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(prompt.Path)
	if err != nil {
		return "", fmt.Errorf("resolve prompt path %q: %w", prompt.Path, err)
	}
	inside, err := pathInside(root, path)
	if err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve %s prompt directory %q: %w", prompt.Source, root, err)
	}
	if !inside {
		inside, err = pathInside(resolvedRoot, path)
		if err != nil {
			return "", err
		}
	}
	if !inside {
		return "", fmt.Errorf("prompt path %q is outside %s prompt directory %q", prompt.Path, prompt.Source, root)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect prompt path %q: %w", prompt.Path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("prompt path %q is a symlink", prompt.Path)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve prompt path %q: %w", prompt.Path, err)
	}
	inside, err = pathInside(resolvedRoot, resolvedPath)
	if err != nil {
		return "", err
	}
	if !inside {
		return "", fmt.Errorf("prompt path %q is outside %s prompt directory %q", prompt.Path, prompt.Source, root)
	}
	return path, nil
}

func (libraries Libraries) root(source string) (string, error) {
	var root string
	switch source {
	case SourceProject:
		root = libraries.LocalDir
	case SourceGlobal:
		root = libraries.GlobalDir
	default:
		return "", fmt.Errorf("unknown prompt source %q", source)
	}
	if root == "" {
		return "", fmt.Errorf("%s prompt directory is not configured", source)
	}
	return filepath.Abs(root)
}

func pathInside(root, path string) (bool, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false, err
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative), nil
}

func encodePrompt(title, description, body string) ([]byte, error) {
	metadata := map[string]string{"title": title, "description": description}
	encoded, err := yaml.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	return append(append([]byte("---\n"), encoded...), append([]byte("---\n"), []byte(body)...)...), nil
}

func updateMetadata(frontmatter, newline []byte, document *yaml.Node, title, description string) ([]byte, error) {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("frontmatter must be a mapping")
	}
	mapping := document.Content[0]
	type replacement struct {
		start int
		end   int
		value []byte
	}
	lineOffsets := []int{0}
	for index := 0; index < len(frontmatter); {
		if next := bytes.Index(frontmatter[index:], newline); next >= 0 {
			index += next + len(newline)
			lineOffsets = append(lineOffsets, index)
			continue
		}
		break
	}
	fields := []struct {
		key   string
		value string
	}{{"title", title}, {"description", description}}
	var replacements []replacement
	for index := 0; index < len(mapping.Content); index += 2 {
		key := mapping.Content[index]
		fieldIndex := -1
		for candidate, field := range fields {
			if field.key == key.Value {
				fieldIndex = candidate
				break
			}
		}
		if fieldIndex < 0 {
			continue
		}
		if key.Line < 1 || key.Line > len(lineOffsets) {
			return nil, fmt.Errorf("locate %s field", key.Value)
		}
		end := len(frontmatter)
		if index+2 < len(mapping.Content) {
			nextLine := mapping.Content[index+2].Line
			if nextLine >= 1 && nextLine <= len(lineOffsets) {
				end = lineOffsets[nextLine-1]
			}
		}
		encoded, err := yaml.Marshal(fields[fieldIndex].value)
		if err != nil {
			return nil, err
		}
		encoded = bytes.TrimSuffix(encoded, []byte("\n"))
		encoded = bytes.ReplaceAll(encoded, []byte("\n"), newline)
		replacementValue := append([]byte(key.Value+": "), encoded...)
		if end < len(frontmatter) {
			replacementValue = append(replacementValue, newline...)
		}
		replacements = append(replacements, replacement{lineOffsets[key.Line-1], end, replacementValue})
		fields = append(fields[:fieldIndex], fields[fieldIndex+1:]...)
	}
	for index := len(replacements) - 1; index >= 0; index-- {
		replacement := replacements[index]
		frontmatter = append(append(append([]byte{}, frontmatter[:replacement.start]...), replacement.value...), frontmatter[replacement.end:]...)
	}
	for _, field := range fields {
		encoded, err := yaml.Marshal(field.value)
		if err != nil {
			return nil, err
		}
		encoded = bytes.TrimSuffix(encoded, []byte("\n"))
		encoded = bytes.ReplaceAll(encoded, []byte("\n"), newline)
		if len(frontmatter) > 0 {
			frontmatter = append(frontmatter, newline...)
		}
		frontmatter = append(frontmatter, []byte(field.key+": ")...)
		frontmatter = append(frontmatter, encoded...)
	}
	return frontmatter, nil
}

var invalidSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(title string) string {
	value := strings.ToLower(strings.TrimSpace(title))
	value = invalidSlug.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "prompt"
	}
	return value
}

var fsErrExists = os.ErrExist

func atomicCreate(path string, contents []byte) (bool, error) {
	temporary, err := createTempFile(filepath.Dir(path), ".prompt-*.tmp")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = removeFile(temporaryPath) }()
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return false, err
	}
	if err := temporary.Close(); err != nil {
		return false, err
	}
	if err := linkFile(temporaryPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, fsErrExists
		}
		return false, err
	}
	return true, nil
}

func atomicReplace(path string, contents []byte) error {
	temporary, err := createTempFile(filepath.Dir(path), ".prompt-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = removeFile(temporaryPath) }()
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return renameFile(temporaryPath, path)
}
