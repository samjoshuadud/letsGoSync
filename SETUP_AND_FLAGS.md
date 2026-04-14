# Moodle Todoist CLI: Setup + Flags Tutorial

This guide explains how to set up the Go CLI and how to use **every flag**.

## 1. Build the CLI

```bash
cd /home/punisher/Documents/moodleextension/golang-cli
go build -o moodletodo ./cmd/moodletodo
./moodletodo --help
```

## 2. First-time setup

### 2.1 Configure base values

```bash
./moodletodo config set \
  --moodle-base-url https://tbl.umak.edu.ph \
  --todoist-project "School Assignments"
```

### 2.2 Save Todoist token

```bash
./moodletodo auth todoist --set "<TODOIST_TOKEN>"
```

### 2.3 Moodle auth (Google SSO campuses)

If your Moodle is Google-auth-only, use **session cookie mode**:

1. Log in to Moodle in browser.
2. Open DevTools -> Application/Storage -> Cookies -> `https://tbl.umak.edu.ph`.
3. Copy `MoodleSession` cookie value.
4. Save it:

```bash
./moodletodo auth session --set "MoodleSession=<COOKIE_VALUE>"
```

Then run:

```bash
./moodletodo auth status
./moodletodo run
```

## 3. Daily use

```bash
./moodletodo run         # scrape + sync
./moodletodo scrape      # scrape only
./moodletodo sync        # sync only (from local DB)
./moodletodo status      # local stats
```

---

## 4. Global flags (available on all commands)

| Flag | What it does | Example |
| --- | --- | --- |
| `--config string` | Use custom config file path instead of default `~/.config/moodletodo/config.json` | `./moodletodo --config /tmp/myconfig.json status` |
| `--json` | Return machine-readable JSON output when supported | `./moodletodo --json status` |
| `-h`, `--help` | Show command help | `./moodletodo scrape --help` |

---

## 5. Command flags tutorial (every command)

## `auth login`

Used for username/password Moodle logins (not typical for Google SSO-only sites).

| Flag | What it does | Example |
| --- | --- | --- |
| `--username string` | Moodle username | `./moodletodo auth login --username student@umak.edu.ph --password 'secret'` |
| `--password string` | Moodle password | same as above |
| `-h`, `--help` | Help | `./moodletodo auth login --help` |

## `auth token`

Manage Moodle web-service token.

| Flag | What it does | Example |
| --- | --- | --- |
| `--set string` | Save Moodle web-service token into OS keyring | `./moodletodo auth token --set "<MOODLE_WS_TOKEN>"` |
| `--clear` | Delete stored Moodle token | `./moodletodo auth token --clear` |
| `--reveal` | Print full token instead of masked output | `./moodletodo auth token --reveal` |
| `-h`, `--help` | Help | `./moodletodo auth token --help` |

## `auth session`

Manage Moodle session-cookie fallback (recommended for Google SSO-only Moodle).

| Flag | What it does | Example |
| --- | --- | --- |
| `--set string` | Save Moodle session cookie value into OS keyring | `./moodletodo auth session --set "MoodleSession=..."` |
| `--clear` | Delete stored session cookie | `./moodletodo auth session --clear` |
| `--reveal` | Print full cookie instead of masked output | `./moodletodo auth session --reveal` |
| `-h`, `--help` | Help | `./moodletodo auth session --help` |

## `auth todoist`

Manage Todoist token.

| Flag | What it does | Example |
| --- | --- | --- |
| `--set string` | Save Todoist token into OS keyring | `./moodletodo auth todoist --set "<TODOIST_TOKEN>"` |
| `--clear` | Delete stored Todoist token | `./moodletodo auth todoist --clear` |
| `--reveal` | Print full token instead of masked output | `./moodletodo auth todoist --reveal` |
| `-h`, `--help` | Help | `./moodletodo auth todoist --help` |

## `auth status`

No command-specific flags besides help.

| Flag | What it does | Example |
| --- | --- | --- |
| `-h`, `--help` | Help | `./moodletodo auth status --help` |

## `config show`

No command-specific flags besides help.

| Flag | What it does | Example |
| --- | --- | --- |
| `-h`, `--help` | Help | `./moodletodo config show --help` |

## `config set`

Update config values.

| Flag | What it does | Example |
| --- | --- | --- |
| `--moodle-base-url string` | Moodle base URL | `./moodletodo config set --moodle-base-url https://tbl.umak.edu.ph` |
| `--todoist-project string` | Todoist project for sync target | `./moodletodo config set --todoist-project "School Assignments"` |
| `--db-path string` | SQLite path for local state | `./moodletodo config set --db-path /path/state.db` |
| `--use-exact-date string` | `true/false`: exact Moodle due date vs smart reminder mode | `./moodletodo config set --use-exact-date true` |
| `--include-lessons string` | `true/false`: include lesson/resource-like URL items in scraping | `./moodletodo config set --include-lessons true` |
| `-h`, `--help` | Help | `./moodletodo config set --help` |

## `scrape`

| Flag | What it does | Example |
| --- | --- | --- |
| `--include-lessons` | Include lesson/resource-like URL modules for this run | `./moodletodo scrape --include-lessons` |
| `-h`, `--help` | Help | `./moodletodo scrape --help` |

## `sync`

No command-specific flags besides help.

| Flag | What it does | Example |
| --- | --- | --- |
| `-h`, `--help` | Help | `./moodletodo sync --help` |

## `run`

| Flag | What it does | Example |
| --- | --- | --- |
| `--include-lessons` | Include lesson/resource-like URL modules for this run | `./moodletodo run --include-lessons` |
| `-h`, `--help` | Help | `./moodletodo run --help` |

## `status`

No command-specific flags besides help.

| Flag | What it does | Example |
| --- | --- | --- |
| `-h`, `--help` | Help | `./moodletodo status --help` |

## `completion`

| Flag | What it does | Example |
| --- | --- | --- |
| `-h`, `--help` | Help | `./moodletodo completion --help` |

### Completion subcommands: `completion bash|zsh|fish|powershell`

| Flag | What it does | Example |
| --- | --- | --- |
| `--no-descriptions` | Disable completion descriptions in generated script | `./moodletodo completion bash --no-descriptions` |
| `-h`, `--help` | Help | `./moodletodo completion zsh --help` |

---

## 6. Practical examples

```bash
# Use JSON output for automation
./moodletodo --json run

# Use alternate config file
./moodletodo --config /tmp/moodle-cli.json status

# Switch to exact Moodle deadline mode
./moodletodo config set --use-exact-date true

# Scrape once with lessons included (without changing saved config)
./moodletodo scrape --include-lessons
```
