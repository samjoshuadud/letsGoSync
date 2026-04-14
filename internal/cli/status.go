package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"moodletodo/internal/secrets"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show local sync status",
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

			stats, err := st.Stats()
			if err != nil {
				return err
			}

			secretStore := secrets.NewStore()
			session, _ := secretStore.GetMoodleSessionCookie()
			moodleToken, _ := secretStore.GetMoodleWebServiceToken()
			todoistToken, _ := secretStore.GetTodoistToken()

			if opts.JSON {
				return printJSON(map[string]any{
					"config_path": opts.ConfigPath,
					"config":      cfg,
					"stats":       stats,
					"auth": map[string]any{
						"moodle_session_cookie_present": session != "",
						"moodle_web_token_present":      moodleToken != "",
						"todoist_token_present":         todoistToken != "",
					},
				})
			}

			fmt.Printf("Config: %s\n", opts.ConfigPath)
			fmt.Printf("Moodle base URL: %s\n", cfg.MoodleBaseURL)
			fmt.Printf("Todoist project: %s\n", cfg.TodoistProjectName)
			fmt.Printf("Assignments: %d\n", stats.TotalAssignments)
			if len(stats.AssignmentsByStatus) > 0 {
				fmt.Println("By status:")
				for k, v := range stats.AssignmentsByStatus {
					fmt.Printf("  - %s: %d\n", k, v)
				}
			}
			fmt.Printf("Last merge: %s\n", fmtTime(stats.LastMergeAt))
			fmt.Printf("Last sync: %s\n", fmtTime(stats.LastSyncAt))
			fmt.Printf("Auth -> Moodle session: %t, Moodle token: %t, Todoist token: %t\n",
				session != "", moodleToken != "", todoistToken != "")
			return nil
		},
	}
}

func fmtTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return t.Local().Format(time.RFC3339)
}
