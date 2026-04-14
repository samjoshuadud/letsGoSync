package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"moodletodo/internal/moodle"
	"moodletodo/internal/secrets"
	"moodletodo/internal/todoist"
)

func newAuthCmd() *cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage Moodle and Todoist credentials",
	}
	authCmd.AddCommand(newAuthStatusCmd())
	authCmd.AddCommand(newAuthLoginCmd())
	authCmd.AddCommand(newAuthTokenCmd())
	authCmd.AddCommand(newAuthSessionCmd())
	authCmd.AddCommand(newAuthTodoistCmd())
	return authCmd
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show auth credential status",
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
			todoistToken, err := secretStore.GetTodoistToken()
			if err != nil {
				return err
			}

			type out struct {
				MoodleSessionCookiePresent bool   `json:"moodle_session_cookie_present"`
				MoodleWebTokenPresent      bool   `json:"moodle_web_token_present"`
				TodoistTokenPresent        bool   `json:"todoist_token_present"`
				MoodleSessionValid         bool   `json:"moodle_session_valid"`
				MoodleTokenValid           bool   `json:"moodle_token_valid"`
				TodoistTokenValid          bool   `json:"todoist_token_valid"`
				BaseURL                    string `json:"base_url"`
			}
			resp := out{
				MoodleSessionCookiePresent: sessionCookie != "",
				MoodleWebTokenPresent:      webToken != "",
				TodoistTokenPresent:        todoistToken != "",
				BaseURL:                    cfg.MoodleBaseURL,
			}

			moodleClient := moodle.NewClient(cfg.MoodleBaseURL)
			if sessionCookie != "" {
				moodleClient.SetSessionCookie(sessionCookie)
				resp.MoodleSessionValid = moodleClient.ValidateSession(context.Background()) == nil
			}
			if webToken != "" {
				moodleClient.SetWebServiceToken(webToken)
				resp.MoodleTokenValid = moodleClient.ValidateToken(context.Background()) == nil
			}
			if todoistToken != "" {
				resp.TodoistTokenValid = todoist.NewClient(todoistToken).TestConnection(context.Background()) == nil
			}

			if opts.JSON {
				return printJSON(resp)
			}

			fmt.Printf("Moodle base URL: %s\n", resp.BaseURL)
			fmt.Printf("Moodle session cookie: %t (valid: %t)\n", resp.MoodleSessionCookiePresent, resp.MoodleSessionValid)
			fmt.Printf("Moodle web-service token: %t (valid: %t)\n", resp.MoodleWebTokenPresent, resp.MoodleTokenValid)
			fmt.Printf("Todoist token: %t (valid: %t)\n", resp.TodoistTokenPresent, resp.TodoistTokenValid)
			return nil
		},
	}
}

func newAuthLoginCmd() *cobra.Command {
	var username string
	var password string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to Moodle and store session cookie",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
				return errors.New("username and password are required")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			client := moodle.NewClient(cfg.MoodleBaseURL)
			cookie, err := client.Login(context.Background(), username, password)
			if err != nil {
				return err
			}
			if err := secrets.NewStore().SetMoodleSessionCookie(cookie); err != nil {
				return err
			}

			if opts.JSON {
				return printJSON(map[string]any{
					"ok":             true,
					"session_cookie": secrets.Mask(cookie),
				})
			}
			fmt.Printf("Stored Moodle session cookie: %s\n", secrets.Mask(cookie))
			return nil
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "Moodle username")
	cmd.Flags().StringVar(&password, "password", "", "Moodle password")
	return cmd
}

func newAuthTokenCmd() *cobra.Command {
	var setValue string
	var clear bool
	var reveal bool
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Set/get Moodle web-service token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store := secrets.NewStore()
			if clear {
				return store.DeleteMoodleWebServiceToken()
			}
			if strings.TrimSpace(setValue) != "" {
				return store.SetMoodleWebServiceToken(setValue)
			}
			token, err := store.GetMoodleWebServiceToken()
			if err != nil {
				return err
			}
			if token == "" {
				return errors.New("no Moodle web-service token saved")
			}
			if !reveal {
				token = secrets.Mask(token)
			}
			if opts.JSON {
				return printJSON(map[string]any{"token": token})
			}
			fmt.Println(token)
			return nil
		},
	}
	cmd.Flags().StringVar(&setValue, "set", "", "Set Moodle web-service token")
	cmd.Flags().BoolVar(&clear, "clear", false, "Delete stored Moodle web-service token")
	cmd.Flags().BoolVar(&reveal, "reveal", false, "Print full token value")
	return cmd
}

func newAuthSessionCmd() *cobra.Command {
	var setValue string
	var clear bool
	var reveal bool
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Set/get Moodle session cookie",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store := secrets.NewStore()
			if clear {
				return store.DeleteMoodleSessionCookie()
			}
			if strings.TrimSpace(setValue) != "" {
				return store.SetMoodleSessionCookie(setValue)
			}
			cookie, err := store.GetMoodleSessionCookie()
			if err != nil {
				return err
			}
			if cookie == "" {
				return errors.New("no Moodle session cookie saved")
			}
			out := cookie
			if !reveal {
				out = secrets.Mask(cookie)
			}
			if opts.JSON {
				return printJSON(map[string]any{"session_cookie": out})
			}
			fmt.Println(out)
			return nil
		},
	}
	cmd.Flags().StringVar(&setValue, "set", "", "Set Moodle session cookie value")
	cmd.Flags().BoolVar(&clear, "clear", false, "Delete stored Moodle session cookie")
	cmd.Flags().BoolVar(&reveal, "reveal", false, "Print full cookie value")
	return cmd
}

func newAuthTodoistCmd() *cobra.Command {
	var setValue string
	var clear bool
	var reveal bool
	cmd := &cobra.Command{
		Use:   "todoist",
		Short: "Set/get Todoist token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store := secrets.NewStore()
			if clear {
				return store.DeleteTodoistToken()
			}
			if strings.TrimSpace(setValue) != "" {
				return store.SetTodoistToken(setValue)
			}
			token, err := store.GetTodoistToken()
			if err != nil {
				return err
			}
			if token == "" {
				return errors.New("no Todoist token saved")
			}
			out := token
			if !reveal {
				out = secrets.Mask(token)
			}
			if opts.JSON {
				return printJSON(map[string]any{"todoist_token": out})
			}
			fmt.Println(out)
			return nil
		},
	}
	cmd.Flags().StringVar(&setValue, "set", "", "Set Todoist token")
	cmd.Flags().BoolVar(&clear, "clear", false, "Delete stored Todoist token")
	cmd.Flags().BoolVar(&reveal, "reveal", false, "Print full token value")
	return cmd
}
