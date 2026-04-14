# Moodle -> Todoist Go CLI

This folder contains the Go CLI rewrite of the Moodle extension workflow.

Detailed setup + full flag tutorial: [`SETUP_AND_FLAGS.md`](./SETUP_AND_FLAGS.md).

The CLI supports:
- Moodle authentication via **web-service token** or **session-cookie fallback**
- local persistence with SQLite
- Todoist sync with dedupe/skip behavior compatible with the extension
- secure secret storage via OS keyring

## Quick start

1. Build:

```bash
cd golang-cli
go build -o moodletodo ./cmd/moodletodo
```

2. Configure:

```bash
./moodletodo config set --moodle-base-url https://tbl.umak.edu.ph --todoist-project "School Assignments"
```

3. Save credentials:

```bash
# Moodle web-service token (if available)
./moodletodo auth token --set "<MOODLE_WS_TOKEN>"

# OR Moodle session cookie fallback
./moodletodo auth session --set "MoodleSession=..."

# Todoist token
./moodletodo auth todoist --set "<TODOIST_TOKEN>"
```

4. Run scrape + sync:

```bash
./moodletodo run
```

## Command reference

```bash
./moodletodo auth status
./moodletodo auth login --username "<USER>" --password "<PASS>"
./moodletodo auth token --set "<MOODLE_WS_TOKEN>"
./moodletodo auth session --set "MoodleSession=..."
./moodletodo auth todoist --set "<TODOIST_TOKEN>"

./moodletodo config show
./moodletodo config set --use-exact-date true

./moodletodo scrape
./moodletodo sync
./moodletodo run
./moodletodo status
```

## Migration notes (extension -> CLI)

1. The extension's `chrome.storage.local` assignment cache is replaced by SQLite (`database_path` in config).
2. The extension popup settings map to `config set` and `auth` commands.
3. The Todoist sync strategy is preserved:
   - map local `task_id` into Todoist task description,
   - avoid re-creating tasks already completed/deleted in Todoist,
   - classify results into `added`, `updated`, and `skipped` buckets.
4. Smart reminder vs exact Moodle date behavior is controlled by `use_exact_date`.

## Config file location

By default, config is created under your OS user config directory:

- Linux: `~/.config/moodletodo/config.json`

You can override config location with `--config`.
