# Dynamic Reminder System

A Go service that applies configurable reminder rules to tasks, dispatches
reminders from a concurrent background scheduler, and records every change and
every reminder in an audit trail.

## Tech stack

- **Go 1.25** — API and scheduler
- **PostgreSQL** — persistence (via `pgx/v5` connection pool)
- **chi** — HTTP router / middleware

## Architecture

```
cmd/service        process entry point, wiring, graceful shutdown
internal/config    environment-variable configuration
internal/handler   HTTP layer — thin shells over services
internal/service   business logic; rule mutations + audit in one transaction
internal/repository data access (pgx); works on a pool or a tx
internal/models    domain types
migrations         SQL schema + seed data
```

Each rule mutation and each reminder dispatch writes its audit-log row **inside
the same transaction** as the state change, so the audit trail can never drift
from reality.

## Prerequisites

- Go 1.25+
- A running PostgreSQL instance

## Setup

1. **Create the database:**

   ```sh
   createdb reminders
   ```

2. **Apply the schema and seed data** (creates tables, indexes, and 5 sample
   tasks with reminder rules):

   ```sh
   psql -d reminders -f migrations/001_init.sql
   ```

   The migration is idempotent — safe to re-run.

3. **Configure environment:**

   ```sh
   cp .env.example .env
   # then edit .env and set DATABASE_URL for your local Postgres
   ```

4. **Run:**

   ```sh
   go run ./cmd/service
   ```

   The HTTP server listens on `:8080` and the scheduler ticks every 30s
   (configurable). You will see `⏰ REMINDER` lines on stdout as reminders fire.

## Configuration

All settings are read from environment variables (see `.env.example`):

| Variable               | Default | Description                          |
|------------------------|---------|--------------------------------------|
| `DATABASE_URL`         | —       | Postgres connection string (required)|
| `HTTP_ADDR`            | `:8080` | HTTP listen address                  |
| `SCHEDULER_TICK`       | `30s`   | How often the scheduler scans rules  |
| `SCHEDULER_WORKERS`    | `5`     | Concurrent reminder-dispatch workers |
| `SCHEDULER_QUEUE_SIZE` | `100`   | Buffered job-channel capacity        |
| `SHUTDOWN_TIMEOUT`     | `15s`   | Graceful HTTP shutdown budget        |

## API

Base path: `/api/v1`

| Method | Path                 | Description                          |
|--------|----------------------|--------------------------------------|
| GET    | `/healthz`           | Health check                         |
| GET    | `/api/v1/tasks`      | List seeded tasks                    |
| GET    | `/api/v1/rules`      | List reminder rules                  |
| POST   | `/api/v1/rules`      | Create a reminder rule               |
| GET    | `/api/v1/rules/{id}` | Get one rule                         |
| PATCH  | `/api/v1/rules/{id}` | Update interval and/or active flag   |
| DELETE | `/api/v1/rules/{id}` | Delete a rule                        |
| PATCH  | `/api/v1/rules/{id}/status` | Activate / deactivate a rule  |
| GET    | `/api/v1/audit-logs` | List audit trail                     |

`audit-logs` accepts optional filters: `event_type`, `rule_id`, `task_id`,
`limit`.

### Example requests

```sh
# List tasks (grab a task_id from here)
curl localhost:8080/api/v1/tasks

# Create a rule that reminds every 30 seconds
curl -X POST localhost:8080/api/v1/rules \
  -H 'Content-Type: application/json' \
  -d '{"task_id":"<TASK_UUID>","trigger_interval":"30s","active":true}'

# Deactivate a rule
curl -X PATCH localhost:8080/api/v1/rules/<RULE_UUID>/status \
  -H 'Content-Type: application/json' -d '{"active":false}'

# View the audit trail
curl 'localhost:8080/api/v1/audit-logs?event_type=REMINDER_TRIGGERED&limit=20'
```

## How the scheduler works

- A `time.Ticker` scans for **active rules whose task is not yet overdue** and
  whose `trigger_interval` has elapsed since `last_triggered_at`.
- Due rules are fanned out over a buffered channel to a pool of workers, so a
  slow worker can never block the ticker.
- Each worker advances `last_triggered_at` with a **compare-and-swap** update:
  if two overlapping ticks pick up the same rule, only the first worker claims
  it — the duplicate is skipped instead of reminding twice.
- The `last_triggered_at` update and the `REMINDER_TRIGGERED` audit row commit
  in one transaction; if it fails, the rule is simply retried on the next tick.

## Notes / trade-offs

- `trigger_interval` is stored as a Go duration string (`"30s"`, `"15m"`,
  `"1h"`) so the interval math lives in one place in Go.
- Audit logs intentionally have **no foreign keys** — they must outlive the
  rules and tasks they describe so deletions stay traceable.
- Schema migration is applied manually via `psql` (step 2 above); the app does
  not run migrations on startup.
# dynamic_reminder-
