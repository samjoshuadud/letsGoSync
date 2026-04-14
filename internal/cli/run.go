package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"moodletodo/internal/moodle"
	"moodletodo/internal/secrets"
	"moodletodo/internal/syncer"
	"moodletodo/internal/todoist"
)

func newRunCmd() *cobra.Command {
	var includeLessons bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Scrape from Moodle and sync to Todoist in one command",
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
			todoToken, err := secretStore.GetTodoistToken()
			if err != nil {
				return err
			}
			if todoToken == "" {
				return errors.New("Todoist token is not set; run auth todoist --set <token>")
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

			engine := &syncer.Engine{
				Store:        st,
				Todoist:      todoist.NewClient(todoToken),
				ProjectName:  cfg.TodoistProjectName,
				UseExactDate: cfg.UseExactDate,
			}
			result, err := engine.Sync(context.Background(), assignments)
			if err != nil {
				return err
			}

			totalSkipped := len(result.Skipped.Local) + len(result.Skipped.Todoist) + len(result.Skipped.NoChanges)
			out := map[string]any{
				"scrape": map[string]any{
					"total":    len(assignments),
					"inserted": inserted,
					"updated":  updated,
				},
				"sync": result,
			}
			if opts.JSON {
				return printJSON(out)
			}
			fmt.Printf("Scrape complete. Total: %d (inserted: %d, updated: %d)\n", len(assignments), inserted, updated)
			fmt.Printf("Sync complete. Added: %d, Updated: %d, Skipped: %d, Errors: %d\n",
				len(result.Added), len(result.Updated), totalSkipped, len(result.Errors))
			return nil
		},
		Args: cobra.NoArgs,
		Example: `  moodletodo run
  moodletodo run --include-lessons`,
	}
	cmd.Flags().BoolVar(&includeLessons, "include-lessons", false, "Include lesson/resource-like URL modules")
	return cmd
}
