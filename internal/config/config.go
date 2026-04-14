package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const appName = "moodletodo"

type Config struct {
	MoodleBaseURL      string `json:"moodle_base_url"`
	TodoistProjectName string `json:"todoist_project_name"`
	UseExactDate       bool   `json:"use_exact_date"`
	IncludeLessons     bool   `json:"include_lessons"`
	DatabasePath       string `json:"database_path"`
}

func DefaultAppDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appName), nil
}

func DefaultConfigPath() (string, error) {
	appDir, err := DefaultAppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "config.json"), nil
}

func Defaults(appDir string) Config {
	return Config{
		MoodleBaseURL:      "https://tbl.umak.edu.ph",
		TodoistProjectName: "School Assignments",
		UseExactDate:       false,
		IncludeLessons:     false,
		DatabasePath:       filepath.Join(appDir, "state.db"),
	}
}

func Load(path string) (Config, error) {
	if path == "" {
		return Config{}, errors.New("config path is empty")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Config{}, err
	}

	defaultCfg := Defaults(dir)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := Save(path, defaultCfg); err != nil {
			return Config{}, err
		}
		return defaultCfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg := defaultCfg
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	if cfg.MoodleBaseURL == "" {
		cfg.MoodleBaseURL = defaultCfg.MoodleBaseURL
	}
	if cfg.TodoistProjectName == "" {
		cfg.TodoistProjectName = defaultCfg.TodoistProjectName
	}
	if cfg.DatabasePath == "" {
		cfg.DatabasePath = defaultCfg.DatabasePath
	}

	return cfg, nil
}

func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
