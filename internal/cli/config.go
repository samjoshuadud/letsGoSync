package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"moodletodo/internal/config"
)

func newConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Show or update CLI configuration",
	}
	configCmd.AddCommand(newConfigShowCmd())
	configCmd.AddCommand(newConfigSetCmd())
	return configCmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(cfg)
			}
			fmt.Printf("Config file: %s\n", opts.ConfigPath)
			fmt.Printf("Moodle base URL: %s\n", cfg.MoodleBaseURL)
			fmt.Printf("Todoist project: %s\n", cfg.TodoistProjectName)
			fmt.Printf("Use exact date: %t\n", cfg.UseExactDate)
			fmt.Printf("Include lessons: %t\n", cfg.IncludeLessons)
			fmt.Printf("Database path: %s\n", cfg.DatabasePath)
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	var moodleBaseURL string
	var todoistProject string
	var dbPath string
	var useExactDate string
	var includeLessons string

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Update config values",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			changed := false

			if strings.TrimSpace(moodleBaseURL) != "" {
				cfg.MoodleBaseURL = strings.TrimRight(strings.TrimSpace(moodleBaseURL), "/")
				changed = true
			}
			if strings.TrimSpace(todoistProject) != "" {
				cfg.TodoistProjectName = strings.TrimSpace(todoistProject)
				changed = true
			}
			if strings.TrimSpace(dbPath) != "" {
				cfg.DatabasePath = strings.TrimSpace(dbPath)
				changed = true
			}
			if val, ok, parseErr := parseOptionalBool(useExactDate); parseErr != nil {
				return fmt.Errorf("invalid --use-exact-date value: %w", parseErr)
			} else if ok {
				cfg.UseExactDate = val
				changed = true
			}
			if val, ok, parseErr := parseOptionalBool(includeLessons); parseErr != nil {
				return fmt.Errorf("invalid --include-lessons value: %w", parseErr)
			} else if ok {
				cfg.IncludeLessons = val
				changed = true
			}
			if !changed {
				return errors.New("no config fields provided")
			}
			if err := config.Save(opts.ConfigPath, cfg); err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(map[string]any{"ok": true, "config": cfg})
			}
			fmt.Println("Config updated.")
			return nil
		},
	}
	cmd.Flags().StringVar(&moodleBaseURL, "moodle-base-url", "", "Set Moodle base URL")
	cmd.Flags().StringVar(&todoistProject, "todoist-project", "", "Set Todoist project name")
	cmd.Flags().StringVar(&dbPath, "db-path", "", "Set SQLite database path")
	cmd.Flags().StringVar(&useExactDate, "use-exact-date", "", "Set use_exact_date (true|false)")
	cmd.Flags().StringVar(&includeLessons, "include-lessons", "", "Set include_lessons (true|false)")
	return cmd
}
