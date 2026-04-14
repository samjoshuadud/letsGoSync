package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"moodletodo/internal/moodle"
	"moodletodo/internal/secrets"
)

func newScrapeCmd() *cobra.Command {
	var includeLessons bool
	cmd := &cobra.Command{
		Use:   "scrape",
		Short: "Scrape assignments from Moodle and save to local store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			secretStore := secrets.NewStore()
			sessionCookie, err := secretStore.GetMoodleSessionCookie()
			if err != nil {
				return err
			}
			webToken, err := secretStore.GetMoodleWebServiceToken()
			if err != nil {
				return err
			}
			if sessionCookie == "" && webToken == "" {
				return errors.New("missing Moodle auth: set session cookie or web-service token first")
			}

			client := moodle.NewClient(cfg.MoodleBaseURL)
			if sessionCookie != "" {
				client.SetSessionCookie(sessionCookie)
			}
			if webToken != "" {
				client.SetWebServiceToken(webToken)
			}

			assignments, err := client.ScrapeAssignments(context.Background(), cfg.IncludeLessons || includeLessons)
			if err != nil {
				return err
			}

			st, err := openStore(cfg)
			if err != nil {
				return err
			}
			defer st.Close()

			inserted, updated, err := st.MergeAssignments(assignments)
			if err != nil {
				return err
			}

			out := map[string]any{
				"total_scraped": len(assignments),
				"inserted":      inserted,
				"updated":       updated,
			}
			if opts.JSON {
				out["assignments"] = assignments
				return printJSON(out)
			}
			fmt.Printf("Scraped: %d assignments (inserted: %d, updated: %d)\n", len(assignments), inserted, updated)
			return nil
		},
	}
	cmd.Flags().BoolVar(&includeLessons, "include-lessons", false, "Include lesson/resource-like URL modules")
	return cmd
}
