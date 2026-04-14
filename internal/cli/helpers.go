package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"moodletodo/internal/config"
	"moodletodo/internal/store"
)

func loadConfig() (config.Config, error) {
	return config.Load(opts.ConfigPath)
}

func openStore(cfg config.Config) (*store.Store, error) {
	if strings.TrimSpace(cfg.DatabasePath) == "" {
		return nil, errors.New("database path is empty in config")
	}
	return store.Open(cfg.DatabasePath)
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func parseOptionalBool(v string) (bool, bool, error) {
	trim := strings.TrimSpace(v)
	if trim == "" {
		return false, false, nil
	}
	parsed, err := strconv.ParseBool(trim)
	if err != nil {
		return false, false, err
	}
	return parsed, true, nil
}
