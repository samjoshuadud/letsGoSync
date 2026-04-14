package cli

import (
	"os"

	"github.com/spf13/cobra"

	"moodletodo/internal/config"
)

type rootOptions struct {
	ConfigPath string
	JSON       bool
}

var opts rootOptions

func NewRootCmd() (*cobra.Command, error) {
	defaultConfigPath, err := config.DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	opts.ConfigPath = defaultConfigPath

	root := &cobra.Command{
		Use:   "moodletodo",
		Short: "Moodle to Todoist sync CLI",
		Long:  "Scrape Moodle assignments and sync them to Todoist with local persistence.",
	}
	root.PersistentFlags().StringVar(&opts.ConfigPath, "config", opts.ConfigPath, "Path to config file")
	root.PersistentFlags().BoolVar(&opts.JSON, "json", false, "Print machine-readable JSON output when available")

	root.AddCommand(newAuthCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newScrapeCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newStatusCmd())

	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	return root, nil
}
