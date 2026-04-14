package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"moodletodo/internal/secrets"
	"moodletodo/internal/syncer"
	"moodletodo/internal/todoist"
)

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Sync local assignments to Todoist",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			st, err := openStore(cfg)
			if err != nil {
				return err
			}
			defer st.Close()

			assignments, err := st.GetAssignments()
			if err != nil {
				return err
			}
			if len(assignments) == 0 {
				return errors.New("no assignments in local store; run scrape first")
			}

			todoistToken, err := secrets.NewStore().GetTodoistToken()
			if err != nil {
				return err
			}
			if todoistToken == "" {
				return errors.New("Todoist token is not set; run auth todoist --set <token>")
			}

			engine := &syncer.Engine{
				Store:        st,
				Todoist:      todoist.NewClient(todoistToken),
				ProjectName:  cfg.TodoistProjectName,
				UseExactDate: cfg.UseExactDate,
			}
			result, err := engine.Sync(context.Background(), assignments)
			if err != nil {
				return err
			}

			if opts.JSON {
				return printJSON(result)
			}

			totalSkipped := len(result.Skipped.Local) + len(result.Skipped.Todoist) + len(result.Skipped.NoChanges)
			fmt.Printf("Sync complete. Added: %d, Updated: %d, Skipped: %d, Errors: %d\n",
				len(result.Added), len(result.Updated), totalSkipped, len(result.Errors))
			return nil
		},
	}
}
