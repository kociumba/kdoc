package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	DocComment        string            `toml:"doc_comment"`
	IgnoreIndented    bool              `toml:"ignore_indented"`
	ScanRoot          string            `toml:"scan_root"`
	ScanExclusions    []string          `toml:"scan_exclusions"`
	OutputPath        string            `toml:"output_path"`
	ExtensionsToLangs map[string]string `toml:"extensions_to_langs"`
	GitAvatarSize     int               `toml:"git_avatar_size"`
}

var CFG = Config{
	DocComment:        "///",
	IgnoreIndented:    false,
	ScanRoot:          "./",
	ScanExclusions:    []string{"*.md", "*.txt", "*.cmake", "cmake-build-*"},
	OutputPath:        "./docs",
	ExtensionsToLangs: map[string]string{".cpp": "cpp", ".c": "c", ".h": "cpp", ".hpp": "cpp"},
	GitAvatarSize:     40,
}

// testing comment, loads the config
func Load(config_path string) error {
	if _, err := os.Stat(config_path); os.IsNotExist(err) {
		return Save(config_path)
	}

	_, err := toml.DecodeFile(config_path, &CFG)
	return err
}

// testing comment, saves the config
func Save(config_path string) error {
	dir := filepath.Dir(config_path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.Create(config_path)
	if err != nil {
		return err
	}
	defer f.Close()

	return toml.NewEncoder(f).Encode(CFG)
}
