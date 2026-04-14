package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"

	"moodletodo/internal/model"
)

const (
	metaLastMergeAt = "last_merge_at"
	metaLastSyncAt  = "last_sync_at"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) initSchema() error {
	schema := `
CREATE TABLE IF NOT EXISTS assignments (
  task_id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  title_normalized TEXT,
  raw_title TEXT,
  due_date TEXT,
  opening_date TEXT,
  course TEXT,
  course_code TEXT,
  status TEXT,
  activity_type TEXT,
  source TEXT,
  origin_url TEXT,
  added_date TEXT,
  last_updated TEXT
);
CREATE TABLE IF NOT EXISTS archive (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT,
  title TEXT,
  course_code TEXT,
  archived_at INTEGER NOT NULL,
  archive_reason TEXT,
  payload TEXT
);
CREATE TABLE IF NOT EXISTS synced_tasks (
  task_id TEXT PRIMARY KEY,
  todoist_task_id TEXT NOT NULL,
  synced_at INTEGER NOT NULL,
  last_synced_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS metadata (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`

	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) MergeAssignments(items []model.Assignment) (inserted int, updated int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	selectStmt, err := tx.Prepare(`SELECT 1 FROM assignments WHERE task_id = ?`)
	if err != nil {
		return 0, 0, err
	}
	defer selectStmt.Close()

	insertStmt, err := tx.Prepare(`
INSERT INTO assignments (
  task_id, title, title_normalized, raw_title, due_date, opening_date,
  course, course_code, status, activity_type, source, origin_url, added_date, last_updated
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, 0, err
	}
	defer insertStmt.Close()

	updateStmt, err := tx.Prepare(`
UPDATE assignments SET
  title = ?, title_normalized = ?, raw_title = ?, due_date = ?, opening_date = ?,
  course = ?, course_code = ?, status = ?, activity_type = ?, source = ?,
  origin_url = ?, last_updated = ?
WHERE task_id = ?`)
	if err != nil {
		return 0, 0, err
	}
	defer updateStmt.Close()

	now := time.Now().Format("2006-01-02 15:04:05")
	for _, a := range items {
		if a.TaskID == "" || a.Title == "" {
			continue
		}
		if a.AddedDate == "" {
			a.AddedDate = now
		}
		if a.LastUpdatedAt == "" {
			a.LastUpdatedAt = now
		}

		var exists int
		row := selectStmt.QueryRow(a.TaskID)
		if scanErr := row.Scan(&exists); scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
			err = scanErr
			return
		}

		if exists == 1 {
			_, execErr := updateStmt.Exec(
				a.Title, a.TitleNorm, a.RawTitle, a.DueDate, a.OpeningDate,
				a.Course, a.CourseCode, a.Status, a.ActivityType, a.Source,
				a.OriginURL, now, a.TaskID,
			)
			if execErr != nil {
				err = execErr
				return
			}
			updated++
			continue
		}

		_, execErr := insertStmt.Exec(
			a.TaskID, a.Title, a.TitleNorm, a.RawTitle, a.DueDate, a.OpeningDate,
			a.Course, a.CourseCode, a.Status, a.ActivityType, a.Source, a.OriginURL,
			a.AddedDate, now,
		)
		if execErr != nil {
			err = execErr
			return
		}
		inserted++
	}

	if err = s.SetMetaTx(tx, metaLastMergeAt, fmt.Sprintf("%d", time.Now().Unix())); err != nil {
		return 0, 0, err
	}

	err = tx.Commit()
	return
}

func (s *Store) GetAssignments() ([]model.Assignment, error) {
	rows, err := s.db.Query(`
SELECT title, title_normalized, raw_title, due_date, opening_date, course, course_code,
       status, task_id, activity_type, source, origin_url, added_date, last_updated
FROM assignments`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Assignment
	for rows.Next() {
		var a model.Assignment
		if err := rows.Scan(
			&a.Title, &a.TitleNorm, &a.RawTitle, &a.DueDate, &a.OpeningDate,
			&a.Course, &a.CourseCode, &a.Status, &a.TaskID, &a.ActivityType,
			&a.Source, &a.OriginURL, &a.AddedDate, &a.LastUpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) MarkTaskSynced(taskID, todoistTaskID string) error {
	if taskID == "" || todoistTaskID == "" {
		return errors.New("taskID and todoistTaskID are required")
	}
	now := time.Now().Unix()
	_, err := s.db.Exec(`
INSERT INTO synced_tasks(task_id, todoist_task_id, synced_at, last_synced_at)
VALUES(?, ?, ?, ?)
ON CONFLICT(task_id) DO UPDATE SET
  todoist_task_id=excluded.todoist_task_id,
  last_synced_at=excluded.last_synced_at`, taskID, todoistTaskID, now, now)
	return err
}

func (s *Store) GetSyncedTasks() (map[string]model.SyncedTask, error) {
	rows, err := s.db.Query(`SELECT task_id, todoist_task_id, synced_at, last_synced_at FROM synced_tasks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]model.SyncedTask)
	for rows.Next() {
		var item model.SyncedTask
		if err := rows.Scan(&item.TaskID, &item.TodoistTaskID, &item.SyncedAt, &item.LastSyncedAt); err != nil {
			return nil, err
		}
		out[item.TaskID] = item
	}
	return out, rows.Err()
}

func (s *Store) SetLastSyncNow() error {
	return s.SetMeta(metaLastSyncAt, fmt.Sprintf("%d", time.Now().Unix()))
}

func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`
INSERT INTO metadata(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) SetMetaTx(tx *sql.Tx, key, value string) error {
	_, err := tx.Exec(`
INSERT INTO metadata(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) GetMeta(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM metadata WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *Store) Stats() (model.LocalStats, error) {
	stats := model.LocalStats{
		AssignmentsByStatus: map[string]int{},
	}
	rows, err := s.db.Query(`SELECT COALESCE(status, 'Unknown'), COUNT(*) FROM assignments GROUP BY status`)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return stats, err
		}
		stats.AssignmentsByStatus[status] = count
		stats.TotalAssignments += count
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}

	if ts, err := s.GetMeta(metaLastMergeAt); err == nil && ts != "" {
		if t, parseErr := parseUnix(ts); parseErr == nil {
			stats.LastMergeAt = &t
		}
	}
	if ts, err := s.GetMeta(metaLastSyncAt); err == nil && ts != "" {
		if t, parseErr := parseUnix(ts); parseErr == nil {
			stats.LastSyncAt = &t
		}
	}
	return stats, nil
}

func parseUnix(v string) (time.Time, error) {
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(sec, 0), nil
}
