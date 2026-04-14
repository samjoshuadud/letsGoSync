package todoist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"moodletodo/internal/model"
)

const baseURL = "https://api.todoist.com/api/v1"

type Client struct {
	token      string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token: strings.TrimSpace(token),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Due struct {
	Date string `json:"date"`
}

type Task struct {
	ID          string `json:"id"`
	Content     string `json:"content"`
	Description string `json:"description"`
	Due         *Due   `json:"due"`
}

func (p *Project) UnmarshalJSON(data []byte) error {
	var aux struct {
		ID   any    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.ID = anyToString(aux.ID)
	p.Name = aux.Name
	return nil
}

func (t *Task) UnmarshalJSON(data []byte) error {
	var aux struct {
		ID          any    `json:"id"`
		Content     string `json:"content"`
		Description string `json:"description"`
		Due         *Due   `json:"due"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	t.ID = anyToString(aux.ID)
	t.Content = aux.Content
	t.Description = aux.Description
	t.Due = aux.Due
	return nil
}

func (c *Client) TestConnection(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodGet, baseURL+"/projects", nil)
	if err != nil {
		return err
	}
	_, err = c.do(req)
	return err
}

func (c *Client) GetOrCreateProject(ctx context.Context, name string) (string, error) {
	projects, err := c.listProjects(ctx)
	if err != nil {
		return "", err
	}
	for _, project := range projects {
		if project.Name == name {
			return project.ID, nil
		}
	}

	payload := map[string]any{
		"name":  name,
		"color": "blue",
	}
	body, _ := json.Marshal(payload)
	req, err := c.newRequest(ctx, http.MethodPost, baseURL+"/projects", body)
	if err != nil {
		return "", err
	}
	data, err := c.do(req)
	if err != nil {
		return "", err
	}
	var created Project
	if err := json.Unmarshal(data, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func (c *Client) listProjects(ctx context.Context) ([]Project, error) {
	req, err := c.newRequest(ctx, http.MethodGet, baseURL+"/projects", nil)
	if err != nil {
		return nil, err
	}
	data, err := c.do(req)
	if err != nil {
		return nil, err
	}
	return decodeResultList[Project](data)
}

func (c *Client) GetTasks(ctx context.Context, projectID string) ([]Task, error) {
	params := url.Values{}
	params.Set("project_id", projectID)
	endpoint := baseURL + "/tasks?" + params.Encode()
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	data, err := c.do(req)
	if err != nil {
		return nil, err
	}
	return decodeResultList[Task](data)
}

func (c *Client) GetTask(ctx context.Context, taskID string) (Task, error) {
	req, err := c.newRequest(ctx, http.MethodGet, baseURL+"/tasks/"+taskID, nil)
	if err != nil {
		return Task{}, err
	}
	data, err := c.do(req)
	if err != nil {
		return Task{}, err
	}
	var task Task
	err = json.Unmarshal(data, &task)
	return task, err
}

func (c *Client) GetCompletedItems(ctx context.Context, projectID string) ([]Task, error) {
	params := url.Values{}
	params.Set("project_id", projectID)
	params.Set("limit", "200")
	endpoint := baseURL + "/tasks/completed/by_completion_date?" + params.Encode()
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	data, err := c.do(req)
	if err != nil {
		return nil, err
	}

	var wrapper struct {
		Items   []Task `json:"items"`
		Results []Task `json:"results"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	if len(wrapper.Items) > 0 {
		return wrapper.Items, nil
	}
	return wrapper.Results, nil
}

func (c *Client) CreateTask(ctx context.Context, assignment model.Assignment, projectID string, useExactDate bool) (Task, error) {
	body := map[string]any{
		"content":     FormatTaskContent(assignment),
		"description": FormatTaskDescription(assignment),
		"project_id":  projectID,
		"priority":    2,
	}
	if due := ExpectedDueDate(assignment, useExactDate); due != "" {
		body["due_date"] = due
	}
	if assignment.CourseCode != "" {
		body["labels"] = []string{strings.ToLower(assignment.CourseCode)}
	}
	payload, _ := json.Marshal(body)
	req, err := c.newRequest(ctx, http.MethodPost, baseURL+"/tasks", payload)
	if err != nil {
		return Task{}, err
	}
	data, err := c.do(req)
	if err != nil {
		return Task{}, err
	}
	var task Task
	err = json.Unmarshal(data, &task)
	return task, err
}

func (c *Client) UpdateTask(ctx context.Context, taskID string, assignment model.Assignment, useExactDate bool) (Task, error) {
	body := map[string]any{
		"content":     FormatTaskContent(assignment),
		"description": FormatTaskDescription(assignment),
		"priority":    2,
	}
	if due := ExpectedDueDate(assignment, useExactDate); due != "" {
		body["due_date"] = due
	} else {
		body["due_string"] = "no date"
	}
	if assignment.CourseCode != "" {
		body["labels"] = []string{strings.ToLower(assignment.CourseCode)}
	}
	payload, _ := json.Marshal(body)
	req, err := c.newRequest(ctx, http.MethodPost, baseURL+"/tasks/"+taskID, payload)
	if err != nil {
		return Task{}, err
	}
	data, err := c.do(req)
	if err != nil {
		return Task{}, err
	}
	var task Task
	err = json.Unmarshal(data, &task)
	return task, err
}

func (c *Client) newRequest(ctx context.Context, method, endpoint string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("todoist request failed (%s): %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func decodeResultList[T any](data []byte) ([]T, error) {
	var wrapper struct {
		Results []T `json:"results"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Results != nil {
		return wrapper.Results, nil
	}
	var direct []T
	if err := json.Unmarshal(data, &direct); err != nil {
		return nil, err
	}
	return direct, nil
}

func anyToString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case json.Number:
		return x.String()
	default:
		return fmt.Sprintf("%v", x)
	}
}

func FormatTaskContent(assignment model.Assignment) string {
	title := fallback(assignment.Title, "Unknown Assignment")
	courseCode := assignment.CourseCode
	rawTitle := assignment.RawTitle

	var activityMatch, activityName string
	p1 := regexp.MustCompile(`(?i)ACTIVITY\s+(\d+)\s*-\s*([^[]+)`).FindStringSubmatch(rawTitle)
	if len(p1) > 2 {
		activityMatch = "Activity " + p1[1]
		activityName = strings.TrimSpace(p1[2])
	} else {
		p2 := regexp.MustCompile(`(?i)ACTIVITY\s+(\d+)`).FindStringSubmatch(rawTitle)
		if len(p2) > 1 {
			activityMatch = "Activity " + p2[1]
			remaining := regexp.MustCompile(`(?i)ACTIVITY\s+\d+\s*-?\s*`).ReplaceAllString(rawTitle, "")
			activityName = strings.TrimSpace(regexp.MustCompile(`\[\d+\]`).ReplaceAllString(remaining, ""))
		}
	}
	if activityMatch == "" {
		p3 := regexp.MustCompile(`(?i)Activity\s+(\d+)\s*\(([^)]+)\)`).FindStringSubmatch(title)
		if len(p3) > 2 {
			activityMatch = "Activity " + p3[1]
			activityName = strings.TrimSpace(p3[2])
		}
	}

	if courseCode != "" && activityMatch != "" {
		if activityName != "" {
			activityName = strings.TrimSpace(regexp.MustCompile(`\s*\[\d+\]`).ReplaceAllString(activityName, ""))
			return fmt.Sprintf("%s - %s (%s)", courseCode, activityMatch, activityName)
		}
		return fmt.Sprintf("%s - %s", courseCode, activityMatch)
	}
	if courseCode != "" {
		return fmt.Sprintf("%s - %s", courseCode, title)
	}
	return title
}

func FormatTaskDescription(assignment model.Assignment) string {
	parts := make([]string, 0, 8)
	if assignment.DueDate != "" && assignment.DueDate != "No due date" {
		parts = append(parts, "Deadline: "+assignment.DueDate)
	}
	if assignment.OriginURL != "" {
		parts = append(parts, "Link: "+assignment.OriginURL)
	}
	if assignment.Course != "" {
		parts = append(parts, "Course: "+normalizeInline(assignment.Course))
	}
	if assignment.Source != "" {
		parts = append(parts, "Source: "+assignment.Source)
	}
	if assignment.TaskID != "" {
		parts = append(parts, "Task ID: "+assignment.TaskID)
	}
	if assignment.CourseCode != "" {
		parts = append(parts, "Course Code: "+assignment.CourseCode)
	}
	if assignment.ActivityType != "" {
		parts = append(parts, "Type: "+assignment.ActivityType)
	}
	return strings.Join(parts, "\n")
}

var taskIDRegex = regexp.MustCompile(`(?i)task id:\s*(\w+)`)

func ExtractTaskIDFromDescription(description string) string {
	m := taskIDRegex.FindStringSubmatch(description)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func ExpectedDueDate(assignment model.Assignment, useExactDate bool) string {
	if useExactDate {
		d := parseDate(assignment.DueDate)
		if d.IsZero() {
			return ""
		}
		return d.Format("2006-01-02")
	}
	return CalculateReminderDate(assignment)
}

func CalculateReminderDate(assignment model.Assignment) string {
	due := parseDate(assignment.DueDate)
	if due.IsZero() {
		return ""
	}

	reference := due
	refType := "due"
	open := parseDate(assignment.OpeningDate)
	if !open.IsZero() && open.After(reference) {
		reference = open
		refType = "opening"
	}

	today := time.Now().Truncate(24 * time.Hour)
	daysUntil := int(reference.Sub(today).Hours() / 24)
	if daysUntil <= 0 {
		return today.Format("2006-01-02")
	}

	var daysBefore int
	if refType == "opening" {
		switch {
		case daysUntil <= 1:
			daysBefore = 0
		case daysUntil <= 3:
			daysBefore = 1
		case daysUntil <= 7:
			daysBefore = 2
		case daysUntil <= 14:
			daysBefore = 3
		default:
			daysBefore = 7
		}
	} else {
		switch {
		case daysUntil <= 3:
			daysBefore = max(1, daysUntil-1)
		case daysUntil <= 7:
			daysBefore = 3
		case daysUntil <= 14:
			daysBefore = 5
		case daysUntil <= 30:
			daysBefore = 7
		default:
			daysBefore = 14
		}
	}
	reminder := reference.AddDate(0, 0, -daysBefore)
	if reminder.Before(today) && !due.Before(today) {
		reminder = today
	}
	return reminder.Format("2006-01-02")
}

func HasMeaningfulChanges(local model.Assignment, remote Task, useExactDate bool) bool {
	localTitle := strings.ToLower(strings.TrimSpace(FormatTaskContent(local)))
	remoteTitle := strings.ToLower(strings.TrimSpace(remote.Content))
	if localTitle != remoteTitle {
		return true
	}
	expectedDue := ExpectedDueDate(local, useExactDate)
	currentDue := ""
	if remote.Due != nil {
		currentDue = remote.Due.Date
	}
	return expectedDue != currentDue
}

func parseDate(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" || v == "No due date" || v == "No opening date" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02",
		"Monday, 2 January 2006, 3:04 PM",
		"2 January 2006, 3:04 PM",
		"2 January 2006",
		"January 2, 2006",
	}
	for _, layout := range layouts {
		if d, err := time.Parse(layout, v); err == nil {
			return d
		}
	}
	return time.Time{}
}

func fallback(v, d string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return d
	}
	return v
}

func normalizeInline(v string) string {
	return strings.Join(strings.Fields(v), " ")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
