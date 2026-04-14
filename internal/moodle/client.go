package moodle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"moodletodo/internal/model"
)

type Client struct {
	baseURL       string
	httpClient    *http.Client
	sessionCookie string
	webToken      string
}

func NewClient(baseURL string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
	}
}

func (c *Client) SetSessionCookie(cookie string) {
	c.sessionCookie = strings.TrimSpace(cookie)
}

func (c *Client) SetWebServiceToken(token string) {
	c.webToken = strings.TrimSpace(token)
}

func (c *Client) ValidateSession(ctx context.Context) error {
	if c.sessionCookie == "" {
		return errors.New("moodle session cookie is not set")
	}
	body, err := c.getPath(ctx, "/my/courses.php")
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(body), "/login/index.php") {
		return errors.New("session cookie is invalid or expired")
	}
	return nil
}

func (c *Client) ValidateToken(ctx context.Context) error {
	if c.webToken == "" {
		return errors.New("moodle web-service token is not set")
	}
	var resp struct {
		SiteName  string `json:"sitename"`
		UserID    int64  `json:"userid"`
		Exception string `json:"exception"`
		Message   string `json:"message"`
	}
	if err := c.callWS(ctx, "core_webservice_get_site_info", nil, &resp); err != nil {
		return err
	}
	if resp.Exception != "" {
		return fmt.Errorf("%s: %s", resp.Exception, resp.Message)
	}
	if resp.UserID == 0 {
		return errors.New("token validation returned empty user id")
	}
	return nil
}

func (c *Client) Login(ctx context.Context, username, password string) (string, error) {
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return "", errors.New("username and password are required")
	}

	loginPage, err := c.getPath(ctx, "/login/index.php")
	if err != nil {
		return "", err
	}

	logintoken := findLogintoken(loginPage)
	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)
	if logintoken != "" {
		form.Set("logintoken", logintoken)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/login/index.php", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	body := strings.ToLower(string(bodyBytes))
	if strings.Contains(body, "invalid login") || strings.Contains(body, "/login/index.php") {
		return "", errors.New("login failed; check username/password")
	}

	parsedBase, parseErr := url.Parse(c.baseURL)
	if parseErr != nil {
		return "", parseErr
	}
	jarCookies := c.httpClient.Jar.Cookies(parsedBase)
	if len(jarCookies) == 0 {
		jarCookies = resp.Cookies()
	}

	var cookies []string
	for _, ck := range jarCookies {
		if ck.Name == "" || ck.Value == "" {
			continue
		}
		cookies = append(cookies, ck.Name+"="+ck.Value)
	}
	if len(cookies) == 0 {
		return "", errors.New("login succeeded but no session cookies were returned")
	}
	sessionCookie := strings.Join(cookies, "; ")
	c.SetSessionCookie(sessionCookie)
	return sessionCookie, nil
}

func (c *Client) ScrapeAssignments(ctx context.Context, includeLessons bool) ([]model.Assignment, error) {
	if c.sessionCookie != "" {
		return c.scrapeWithSession(ctx, includeLessons)
	}
	if c.webToken != "" {
		return c.scrapeWithToken(ctx)
	}
	return nil, errors.New("no auth credentials available: set moodle session cookie or web-service token")
}

func (c *Client) scrapeWithSession(ctx context.Context, includeLessons bool) ([]model.Assignment, error) {
	body, err := c.getPath(ctx, "/my/courses.php")
	if err != nil {
		return nil, err
	}

	courseLinks, err := parseCourseLinks(c.baseURL, body)
	if err != nil {
		return nil, err
	}
	if len(courseLinks) == 0 {
		fallbackPages := []string{"/my/", "/my/index.php", "/course/index.php?mycourses=1"}
		for _, page := range fallbackPages {
			pageHTML, pageErr := c.getPath(ctx, page)
			if pageErr != nil {
				continue
			}
			pageLinks, parseErr := parseCourseLinks(c.baseURL, pageHTML)
			if parseErr != nil {
				continue
			}
			courseLinks = appendUniqueLinks(courseLinks, pageLinks...)
		}
	}
	if len(courseLinks) == 0 {
		apiLinks, apiErr := c.getCourseLinksFromOverviewAPI(ctx, body)
		if apiErr == nil {
			courseLinks = appendUniqueLinks(courseLinks, apiLinks...)
		}
	}
	if len(courseLinks) == 0 {
		return nil, errors.New("no courses found from Moodle pages; your session may be on a dashboard layout without static course links, or session cookie may be expired")
	}

	type scrapeJob struct {
		link string
	}
	type scrapeResult struct {
		items []model.Assignment
		err   error
		link  string
	}

	workerCount := 6
	if len(courseLinks) < workerCount {
		workerCount = len(courseLinks)
	}
	if workerCount <= 0 {
		workerCount = 1
	}

	jobs := make(chan scrapeJob)
	results := make(chan scrapeResult, len(courseLinks))

	// Global pacing keeps requests moderate even with concurrency.
	requestTicker := time.NewTicker(80 * time.Millisecond)
	defer requestTicker.Stop()

	for i := 0; i < workerCount; i++ {
		go func() {
			for job := range jobs {
				select {
				case <-ctx.Done():
					results <- scrapeResult{err: ctx.Err(), link: job.link}
					continue
				case <-requestTicker.C:
				}

				html, fetchErr := c.getURL(ctx, job.link)
				if fetchErr != nil {
					results <- scrapeResult{err: fetchErr, link: job.link}
					continue
				}
				items, parseErr := c.extractFromCourseHTML(ctx, job.link, html, includeLessons)
				if parseErr != nil {
					results <- scrapeResult{err: parseErr, link: job.link}
					continue
				}
				results <- scrapeResult{items: items, link: job.link}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, link := range courseLinks {
			jobs <- scrapeJob{link: link}
		}
	}()

	var all []model.Assignment
	var failures []string
	for i := 0; i < len(courseLinks); i++ {
		r := <-results
		if r.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", r.link, r.err))
			continue
		}
		all = append(all, r.items...)
	}

	if len(all) == 0 && len(failures) > 0 {
		return nil, fmt.Errorf("failed to scrape all courses; first error: %s", failures[0])
	}
	return dedupeAssignments(all), nil
}

func (c *Client) scrapeWithToken(ctx context.Context) ([]model.Assignment, error) {
	type siteInfo struct {
		UserID    int64  `json:"userid"`
		Exception string `json:"exception"`
		Message   string `json:"message"`
	}
	var info siteInfo
	if err := c.callWS(ctx, "core_webservice_get_site_info", nil, &info); err != nil {
		return nil, err
	}
	if info.Exception != "" {
		return nil, fmt.Errorf("%s: %s", info.Exception, info.Message)
	}

	values := url.Values{}
	values.Set("userid", fmt.Sprintf("%d", info.UserID))
	var courses []struct {
		ID        int64  `json:"id"`
		FullName  string `json:"fullname"`
		ShortName string `json:"shortname"`
	}
	if err := c.callWS(ctx, "core_enrol_get_users_courses", values, &courses); err != nil {
		return nil, err
	}

	courseNames := map[int64]string{}
	courseIDs := make([]int64, 0, len(courses))
	for _, course := range courses {
		courseIDs = append(courseIDs, course.ID)
		if strings.TrimSpace(course.ShortName) != "" {
			courseNames[course.ID] = course.ShortName
		} else {
			courseNames[course.ID] = course.FullName
		}
	}
	if len(courseIDs) == 0 {
		return nil, errors.New("web-service token returned no enrolled courses")
	}

	assignValues := url.Values{}
	for i, id := range courseIDs {
		assignValues.Set(fmt.Sprintf("courseids[%d]", i), fmt.Sprintf("%d", id))
	}
	var assignResp struct {
		Courses []struct {
			ID          int64 `json:"id"`
			Assignments []struct {
				ID                       int64  `json:"id"`
				CMID                     int64  `json:"cmid"`
				Name                     string `json:"name"`
				DueDate                  int64  `json:"duedate"`
				AllowsSubmissionsFrom    int64  `json:"allowsubmissionsfromdate"`
				Exception                string `json:"exception"`
				SubmissionStatement      string `json:"submissionstatement"`
				AlwaysShowDescription    int    `json:"alwaysshowdescription"`
				PreventSubmissionNotInGS int    `json:"preventsubmissionnotingroup"`
			} `json:"assignments"`
		} `json:"courses"`
	}
	if err := c.callWS(ctx, "mod_assign_get_assignments", assignValues, &assignResp); err != nil {
		return nil, err
	}

	quizValues := url.Values{}
	for i, id := range courseIDs {
		quizValues.Set(fmt.Sprintf("courseids[%d]", i), fmt.Sprintf("%d", id))
	}
	var quizResp struct {
		Quizzes []struct {
			ID        int64  `json:"id"`
			Course    int64  `json:"course"`
			CourseMod int64  `json:"coursemodule"`
			Name      string `json:"name"`
			TimeOpen  int64  `json:"timeopen"`
			TimeClose int64  `json:"timeclose"`
		} `json:"quizzes"`
	}
	_ = c.callWS(ctx, "mod_quiz_get_quizzes_by_courses", quizValues, &quizResp)

	now := time.Now().Format("2006-01-02 15:04:05")
	var out []model.Assignment
	for _, course := range assignResp.Courses {
		for _, a := range course.Assignments {
			taskID := fmt.Sprintf("%d", a.CMID)
			if taskID == "0" {
				taskID = fmt.Sprintf("assign-%d", a.ID)
			}
			courseName := courseNames[course.ID]
			due := unixDate(a.DueDate)
			opening := unixDate(a.AllowsSubmissionsFrom)
			originURL := fmt.Sprintf("%s/mod/assign/view.php?id=%s", c.baseURL, taskID)
			out = append(out, model.Assignment{
				Title:         a.Name,
				TitleNorm:     normalizeTitle(a.Name),
				RawTitle:      a.Name,
				DueDate:       dueOrDefault(due),
				OpeningDate:   openingOrDefault(opening),
				Course:        courseName,
				CourseCode:    extractCourseCode(courseName),
				Status:        "Pending",
				TaskID:        taskID,
				ActivityType:  "assign",
				Source:        "moodle_api",
				OriginURL:     originURL,
				AddedDate:     now,
				LastUpdatedAt: now,
			})
		}
	}
	for _, q := range quizResp.Quizzes {
		taskID := fmt.Sprintf("%d", q.CourseMod)
		if taskID == "0" {
			taskID = fmt.Sprintf("quiz-%d", q.ID)
		}
		courseName := courseNames[q.Course]
		originURL := fmt.Sprintf("%s/mod/quiz/view.php?id=%s", c.baseURL, taskID)
		out = append(out, model.Assignment{
			Title:         q.Name,
			TitleNorm:     normalizeTitle(q.Name),
			RawTitle:      q.Name,
			DueDate:       dueOrDefault(unixDate(q.TimeClose)),
			OpeningDate:   openingOrDefault(unixDate(q.TimeOpen)),
			Course:        courseName,
			CourseCode:    extractCourseCode(courseName),
			Status:        "Pending",
			TaskID:        taskID,
			ActivityType:  "quiz",
			Source:        "moodle_api",
			OriginURL:     originURL,
			AddedDate:     now,
			LastUpdatedAt: now,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return dedupeAssignments(out), nil
}

func parseCourseLinks(baseURL, html string) ([]string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var links []string
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || strings.TrimSpace(href) == "" {
			return
		}
		if !strings.Contains(href, "course/view.php?id=") {
			return
		}
		full := resolveURL(baseURL, href)
		if _, exists := seen[full]; exists {
			return
		}
		seen[full] = struct{}{}
		links = append(links, full)
	})
	doc.Find(`[data-course-id]`).Each(func(_ int, s *goquery.Selection) {
		courseID := strings.TrimSpace(s.AttrOr("data-course-id", ""))
		if courseID == "" {
			return
		}
		full := resolveURL(baseURL, "/course/view.php?id="+courseID)
		if _, exists := seen[full]; exists {
			return
		}
		seen[full] = struct{}{}
		links = append(links, full)
	})
	for _, raw := range extractCourseLinksFromRawHTML(html) {
		full := resolveURL(baseURL, raw)
		if _, exists := seen[full]; exists {
			continue
		}
		seen[full] = struct{}{}
		links = append(links, full)
	}
	sort.Strings(links)
	return links, nil
}

func (c *Client) extractFromCourseHTML(ctx context.Context, courseURL, html string, includeLessons bool) ([]model.Assignment, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}
	courseName := strings.TrimSpace(firstNonEmpty(
		doc.Find("#page-header h1").First().Text(),
		doc.Find(".page-header-headings h1").First().Text(),
	))
	now := time.Now().Format("2006-01-02 15:04:05")

	candidates := make([]*goquery.Selection, 0)
	doc.Find(".modtype_assign").Each(func(_ int, s *goquery.Selection) {
		if hasClass(s, "submission_done") {
			return
		}
		candidates = append(candidates, s)
	})
	doc.Find(".modtype_quiz").Each(func(_ int, s *goquery.Selection) {
		if hasClass(s, "quiz_closed") {
			return
		}
		candidates = append(candidates, s)
	})
	doc.Find(".modtype_url").Each(func(_ int, s *goquery.Selection) {
		title := strings.ToLower(strings.TrimSpace(firstNonEmpty(
			s.Find(".instancename").First().Text(),
			s.Find("a").First().Text(),
		)))
		if strings.Contains(title, "quiz") || strings.Contains(title, "exam") ||
			strings.Contains(title, "test") || strings.Contains(title, "assignment") ||
			strings.Contains(title, "assessment") || strings.Contains(title, "midterm") ||
			strings.Contains(title, "final") {
			candidates = append(candidates, s)
			return
		}
		if includeLessons {
			candidates = append(candidates, s)
		}
	})
	doc.Find(".modtype_forum").Each(func(_ int, s *goquery.Selection) {
		if parseDueDate(s.Text(), s.Find("time[datetime]").First().AttrOr("datetime", "")) != "" {
			candidates = append(candidates, s)
		}
	})

	seen := map[string]struct{}{}
	assignments := make([]model.Assignment, 0, len(candidates))
	for _, node := range candidates {
		title := strings.TrimSpace(firstNonEmpty(
			node.Find(".instancename").First().Text(),
			node.Find("a").First().Text(),
		))
		href, _ := node.Find("a").First().Attr("href")
		fullURL := resolveURL(c.baseURL, href)

		if title == "" || fullURL == "" {
			continue
		}
		key := title + "::" + fullURL
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		activityType := detectActivityType(node, title)
		if activityType == "lesson_link" && !includeLessons {
			continue
		}

		dueDate := parseDueDate(node.Text(), node.Find("time[datetime]").First().AttrOr("datetime", ""))
		if dueDate == "" {
			dueDate = "No due date"
		}
		openingDate := parseOpeningDate(node.Text(), node.Find("time[datetime]").First().AttrOr("datetime", ""))
		if openingDate == "" {
			openingDate = "No opening date"
		}

		status := c.getCompletionStatus(ctx, activityType, fullURL, node)
		taskID := taskIDFromURL(fullURL)
		assignments = append(assignments, model.Assignment{
			Title:         title,
			TitleNorm:     normalizeTitle(title),
			RawTitle:      title,
			DueDate:       dueDate,
			OpeningDate:   openingDate,
			Course:        courseName,
			CourseCode:    extractCourseCode(courseName),
			Status:        status,
			TaskID:        taskID,
			ActivityType:  activityType,
			Source:        "scrape",
			OriginURL:     fullURL,
			AddedDate:     now,
			LastUpdatedAt: now,
		})
	}
	return assignments, nil
}

func (c *Client) getCompletionStatus(ctx context.Context, activityType, activityURL string, node *goquery.Selection) string {
	btn := node.Find(`[data-action="toggle-manual-completion"]`).First()
	if btn.Length() > 0 {
		text := strings.ToLower(strings.TrimSpace(btn.Text()))
		toggle := strings.ToLower(strings.TrimSpace(btn.AttrOr("data-toggletype", "")))
		title := strings.ToLower(strings.TrimSpace(btn.AttrOr("title", "")))
		if strings.Contains(text, "mark as done") || strings.Contains(toggle, "manual:mark") || strings.Contains(title, "mark as done") {
			return "Pending"
		}
		if strings.Contains(text, "done") || strings.Contains(toggle, "undo") || strings.Contains(title, "marked as done") || strings.Contains(title, "press to undo") {
			return "Completed"
		}
	}

	if (activityType == "assign" || activityType == "quiz") && activityURL != "" {
		html, err := c.getURL(ctx, activityURL)
		if err != nil {
			return "Pending"
		}
		doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(html))
		if parseErr != nil {
			return "Pending"
		}
		status := "Pending"
		doc.Find("table.generaltable tr").EachWithBreak(func(_ int, tr *goquery.Selection) bool {
			head := strings.ToLower(strings.TrimSpace(tr.Find("th").First().Text()))
			val := strings.ToLower(strings.TrimSpace(tr.Find("td").First().Text()))
			if head == "submission status" {
				if strings.Contains(val, "submitted for grading") || strings.Contains(val, "submitted") {
					status = "Completed"
				}
				return false
			}
			return true
		})
		if status == "Pending" {
			lower := strings.ToLower(html)
			if strings.Contains(lower, "submitted for grading") || strings.Contains(lower, "submission status") && strings.Contains(lower, "submitted") {
				return "Completed"
			}
		}
		return status
	}
	return "Pending"
}

func (c *Client) getPath(ctx context.Context, path string) (string, error) {
	return c.getURL(ctx, c.baseURL+path)
}

func (c *Client) getURL(ctx context.Context, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	if c.sessionCookie != "" {
		req.Header.Set("Cookie", c.sessionCookie)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("request failed (%s): %s", resp.Status, strings.TrimSpace(string(body)))
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *Client) callWS(ctx context.Context, fn string, params url.Values, out any) error {
	values := url.Values{}
	values.Set("wstoken", c.webToken)
	values.Set("wsfunction", fn)
	values.Set("moodlewsrestformat", "json")
	for k, vv := range params {
		for _, v := range vv {
			values.Add(k, v)
		}
	}
	endpoint := c.baseURL + "/webservice/rest/server.php?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webservice request failed (%s): %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("webservice decode failed: %w", err)
	}
	return nil
}

func findLogintoken(html string) string {
	re := regexp.MustCompile(`name="logintoken"\s+value="([^"]+)"`)
	m := re.FindStringSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func findSesskey(html string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`"sesskey"\s*:\s*"([A-Za-z0-9]+)"`),
		regexp.MustCompile(`sesskey["']?\s*[:=]\s*["']([A-Za-z0-9]+)["']`),
		regexp.MustCompile(`name="sesskey"\s+value="([^"]+)"`),
	}
	for _, re := range patterns {
		m := re.FindStringSubmatch(html)
		if len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func extractCourseLinksFromRawHTML(html string) []string {
	unescaped := strings.ReplaceAll(html, `\/`, `/`)
	re := regexp.MustCompile(`(?:https?://[^\s"'<>]+/course/view\.php\?id=\d+|/course/view\.php\?id=\d+|course/view\.php\?id=\d+)`)
	matches := re.FindAllString(unescaped, -1)
	seen := map[string]struct{}{}
	var links []string
	for _, match := range matches {
		if _, exists := seen[match]; exists {
			continue
		}
		seen[match] = struct{}{}
		links = append(links, match)
	}
	return links
}

func appendUniqueLinks(base []string, more ...string) []string {
	seen := map[string]struct{}{}
	for _, link := range base {
		seen[link] = struct{}{}
	}
	for _, link := range more {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		if _, exists := seen[link]; exists {
			continue
		}
		seen[link] = struct{}{}
		base = append(base, link)
	}
	sort.Strings(base)
	return base
}

func (c *Client) getCourseLinksFromOverviewAPI(ctx context.Context, html string) ([]string, error) {
	sesskey := findSesskey(html)
	if sesskey == "" {
		return nil, errors.New("sesskey not found")
	}

	endpoint := c.baseURL + "/lib/ajax/service.php?sesskey=" + url.QueryEscape(sesskey) + "&info=core_course_get_enrolled_courses_by_timeline_classification"
	payload := `[{"index":0,"methodname":"core_course_get_enrolled_courses_by_timeline_classification","args":{"offset":0,"limit":0,"classification":"all","sort":"fullname"}}]`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.sessionCookie != "" {
		req.Header.Set("Cookie", c.sessionCookie)
	}

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
		return nil, fmt.Errorf("overview API request failed (%s)", resp.Status)
	}

	var calls []map[string]any
	if err := json.Unmarshal(data, &calls); err != nil {
		return nil, err
	}

	var links []string
	for _, call := range calls {
		callData, ok := call["data"].(map[string]any)
		if !ok {
			continue
		}
		courses, ok := callData["courses"].([]any)
		if !ok {
			continue
		}
		for _, rawCourse := range courses {
			course, ok := rawCourse.(map[string]any)
			if !ok {
				continue
			}
			viewURL, _ := course["viewurl"].(string)
			if strings.TrimSpace(viewURL) != "" {
				links = append(links, resolveURL(c.baseURL, viewURL))
				continue
			}
			courseID := int64FromAny(course["id"])
			if courseID > 0 {
				links = append(links, resolveURL(c.baseURL, fmt.Sprintf("/course/view.php?id=%d", courseID)))
			}
		}
	}
	links = appendUniqueLinks(nil, links...)
	if len(links) == 0 {
		return nil, errors.New("overview API returned no course links")
	}
	return links, nil
}

func int64FromAny(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	case json.Number:
		i, _ := x.Int64()
		return i
	default:
		return 0
	}
}

func resolveURL(base, href string) string {
	if strings.TrimSpace(href) == "" {
		return ""
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return baseURL.ResolveReference(ref).String()
}

func hasClass(s *goquery.Selection, className string) bool {
	classAttr, ok := s.Attr("class")
	if !ok {
		return false
	}
	for _, c := range strings.Fields(classAttr) {
		if c == className {
			return true
		}
	}
	return false
}

func firstNonEmpty(parts ...string) string {
	for _, part := range parts {
		v := strings.TrimSpace(part)
		if v != "" {
			return v
		}
	}
	return ""
}

func detectActivityType(node *goquery.Selection, title string) string {
	classAttr, _ := node.Attr("class")
	m := regexp.MustCompile(`modtype_([a-zA-Z]+)`).FindStringSubmatch(classAttr)
	modType := "unknown"
	if len(m) > 1 {
		modType = strings.ToLower(m[1])
	}
	if modType == "url" {
		lowerTitle := strings.ToLower(title)
		if strings.Contains(lowerTitle, "quiz") || strings.Contains(lowerTitle, "exam") ||
			strings.Contains(lowerTitle, "test") || strings.Contains(lowerTitle, "assignment") ||
			strings.Contains(lowerTitle, "assessment") || strings.Contains(lowerTitle, "midterm") ||
			strings.Contains(lowerTitle, "final") {
			return "quiz_link"
		}
		return "lesson_link"
	}
	return modType
}

var duePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)Due\s*:?\s*([A-Za-z]+,?\s+\d{1,2}\s+[A-Za-z]+\s+\d{4},?\s*\d{1,2}:\d{2}\s*[APM]{2})`),
	regexp.MustCompile(`(?i)Due\s*:?\s*([A-Za-z]+,?\s+\d{1,2}\s+[A-Za-z]+\s+\d{4})`),
	regexp.MustCompile(`(?i)Due date\s*:?\s*([A-Za-z]+,?\s+\d{1,2}\s+[A-Za-z]+\s+\d{4})`),
	regexp.MustCompile(`(?i)Deadline\s*:?\s*([A-Za-z]+,?\s+\d{1,2}\s+[A-Za-z]+\s+\d{4})`),
	regexp.MustCompile(`(?i)Closes\s*:?\s*([A-Za-z]+,?\s+\d{1,2}\s+[A-Za-z]+\s+\d{4})`),
	regexp.MustCompile(`(?i)Due\s*:?\s*(\d{4}-\d{2}-\d{2})`),
}

var openingPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)Open(?:ing)?\s*:?\s*([A-Za-z]+,?\s+\d{1,2}\s+[A-Za-z]+\s+\d{4},?\s*\d{1,2}:\d{2}\s*[APM]{2})`),
	regexp.MustCompile(`(?i)Open(?:ing)?\s*:?\s*([A-Za-z]+,?\s+\d{1,2}\s+[A-Za-z]+\s+\d{4})`),
	regexp.MustCompile(`(?i)Available from\s*:?\s*([A-Za-z]+,?\s+\d{1,2}\s+[A-Za-z]+\s+\d{4})`),
	regexp.MustCompile(`(?i)Opens\s*:?\s*([A-Za-z]+,?\s+\d{1,2}\s+[A-Za-z]+\s+\d{4})`),
	regexp.MustCompile(`(?i)Open(?:ing)?\s*:?\s*(\d{4}-\d{2}-\d{2})`),
}

func parseDueDate(text, datetimeAttr string) string {
	if iso := parseDatetimeAttr(datetimeAttr); iso != "" {
		return iso
	}
	normalized := normalizeSpaces(text)
	for _, re := range duePatterns {
		m := re.FindStringSubmatch(normalized)
		if len(m) < 2 {
			continue
		}
		if d := parseDateToISO(m[1]); d != "" {
			return d
		}
	}
	return ""
}

func parseOpeningDate(text, datetimeAttr string) string {
	if iso := parseDatetimeAttr(datetimeAttr); iso != "" {
		return iso
	}
	normalized := normalizeSpaces(text)
	for _, re := range openingPatterns {
		m := re.FindStringSubmatch(normalized)
		if len(m) < 2 {
			continue
		}
		if d := parseDateToISO(m[1]); d != "" {
			return d
		}
	}
	return ""
}

func parseDatetimeAttr(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if d := parseDateToISO(v); d != "" {
		return d
	}
	return ""
}

func parseDateToISO(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02",
		"January 2, 2006",
		"2 January 2006",
		"Monday, 2 January 2006",
		"Monday, 2 January 2006, 3:04 PM",
		"January 2, 2006, 3:04 PM",
		"2 January 2006, 3:04 PM",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t.Format("2006-01-02")
		}
	}
	if t, err := time.ParseInLocation("Monday, 2 January 2006, 3:04 PM", v, time.Local); err == nil {
		return t.Format("2006-01-02")
	}
	if t, err := time.ParseInLocation("2 January 2006, 3:04 PM", v, time.Local); err == nil {
		return t.Format("2006-01-02")
	}
	return ""
}

func normalizeSpaces(v string) string {
	return strings.Join(strings.Fields(v), " ")
}

func extractCourseCode(course string) string {
	course = strings.TrimSpace(course)
	if course == "" {
		return ""
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\(([A-Z]{2,10}\d{2,4})\)`),
		regexp.MustCompile(`:\s*([A-Z]{2,10}\d{2,4})`),
		regexp.MustCompile(`^([A-Z]{2,10}\d{2,4})`),
		regexp.MustCompile(`([A-Z]{2,10}\d{2,4})$`),
		regexp.MustCompile(`([A-Z]{2,4}\s?-?\d{3,4})`),
	}
	for _, re := range patterns {
		m := re.FindStringSubmatch(course)
		if len(m) > 1 {
			return strings.ReplaceAll(m[1], " ", "")
		}
	}
	parts := strings.FieldsFunc(course, func(r rune) bool {
		return r == ' ' || r == ':'
	})
	if len(parts) == 0 {
		return ""
	}
	if len(parts[0]) > 12 {
		return parts[0][:12]
	}
	return parts[0]
}

func taskIDFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	id := u.Query().Get("id")
	if id != "" {
		return id
	}
	return raw
}

func normalizeTitle(v string) string {
	return strings.ToLower(normalizeSpaces(v))
}

func dedupeAssignments(items []model.Assignment) []model.Assignment {
	out := make([]model.Assignment, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		if item.TaskID == "" {
			continue
		}
		if _, exists := seen[item.TaskID]; exists {
			continue
		}
		seen[item.TaskID] = struct{}{}
		out = append(out, item)
	}
	return out
}

func unixDate(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("2006-01-02")
}

func dueOrDefault(v string) string {
	if strings.TrimSpace(v) == "" {
		return "No due date"
	}
	return v
}

func openingOrDefault(v string) string {
	if strings.TrimSpace(v) == "" {
		return "No opening date"
	}
	return v
}
