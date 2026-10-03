package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

const herdrPluginConfigDirEnv = "HERDR_PLUGIN_CONFIG_DIR"

type pluginSettings struct {
	InputCommand string `toml:"input_command"`
}

// LoadInputCommand reads input_command from the plugin's config.toml. An
// absent file or setting leaves the command empty so the caller can use its default.
func LoadInputCommand() (string, error) {
	configDirectory := os.Getenv(herdrPluginConfigDirEnv)
	if configDirectory == "" {
		return "", nil
	}

	path := filepath.Join(configDirectory, "config.toml")
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read plugin config %q: %w", path, err)
	}

	var settings pluginSettings
	if err := toml.Unmarshal(contents, &settings); err != nil {
		return "", fmt.Errorf("parse plugin config %q: %w", path, err)
	}
	return settings.InputCommand, nil
}
