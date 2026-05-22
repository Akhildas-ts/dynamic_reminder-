-- =====================================================================
--  Dynamic Reminder System — schema & seed
--  Idempotent: safe to re-run on a fresh or existing database.
-- =====================================================================

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ---------------------------------------------------------------------
--  ENUMs
-- ---------------------------------------------------------------------
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'audit_event_type') THEN
        CREATE TYPE audit_event_type AS ENUM (
            'RULE_CREATE',
            'RULE_UPDATE',
            'RULE_DELETE',
            'STATUS_CHANGE',
            'REMINDER_TRIGGERED'
        );
    END IF;
END$$;

-- ---------------------------------------------------------------------
--  Tables
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tasks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title       TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    due_date    TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS reminder_rules (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id           UUID        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    trigger_interval  TEXT        NOT NULL,           -- Go duration string: "30s", "15m", "1h"
    active            BOOLEAN     NOT NULL DEFAULT TRUE,
    last_triggered_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_rules_active_task ON reminder_rules (active, task_id);

-- Audit logs intentionally have NO foreign keys: they must outlive the
-- rules/tasks they describe so deletions are still traceable.
CREATE TABLE IF NOT EXISTS audit_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type audit_event_type NOT NULL,
    rule_id    UUID,
    task_id    UUID,
    payload    JSONB            NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_created_at ON audit_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_event_type ON audit_logs (event_type);
CREATE INDEX IF NOT EXISTS idx_audit_rule_id    ON audit_logs (rule_id);
CREATE INDEX IF NOT EXISTS idx_audit_task_id    ON audit_logs (task_id);

-- ---------------------------------------------------------------------
--  Seed: 5 distinct tasks with varying due dates
-- ---------------------------------------------------------------------
INSERT INTO tasks (id, title, description, due_date) VALUES
    ('11111111-1111-1111-1111-111111111111',
     'Submit Q2 financial report',
     'Compile and submit the quarterly P&L statements.',
     NOW() + INTERVAL '2 days'),
    ('22222222-2222-2222-2222-222222222222',
     'Deploy v2.1 to production',
     'Coordinate release with SRE and roll out v2.1.',
     NOW() + INTERVAL '6 hours'),
    ('33333333-3333-3333-3333-333333333333',
     'Doctor appointment',
     'Annual physical at the downtown clinic.',
     NOW() + INTERVAL '7 days'),
    ('44444444-4444-4444-4444-444444444444',
     'Renew domain certificate',
     'Renew SSL cert for api.example.com before expiry.',
     NOW() + INTERVAL '30 days'),
    ('55555555-5555-5555-5555-555555555555',
     'Team sync meeting',
     'Weekly engineering sync to align on roadmap.',
     NOW() + INTERVAL '1 day')
ON CONFLICT (id) DO NOTHING;

-- One rule per task with intentionally varying intervals so the scheduler
-- demonstrates concurrent dispatch from the very first tick.
INSERT INTO reminder_rules (task_id, trigger_interval, active)
SELECT t.id, v.trigger_interval, v.active
FROM (VALUES
    ('11111111-1111-1111-1111-111111111111'::uuid, '30s', TRUE),
    ('22222222-2222-2222-2222-222222222222'::uuid, '1m',  TRUE),
    ('33333333-3333-3333-3333-333333333333'::uuid, '5m',  TRUE),
    ('44444444-4444-4444-4444-444444444444'::uuid, '1h',  FALSE),
    ('55555555-5555-5555-5555-555555555555'::uuid, '2m',  TRUE)
) AS v(task_id, trigger_interval, active)
JOIN tasks t ON t.id = v.task_id
WHERE NOT EXISTS (
    SELECT 1 FROM reminder_rules rr WHERE rr.task_id = v.task_id
);
