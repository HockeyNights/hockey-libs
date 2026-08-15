-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS outbox (
    id              uuid        PRIMARY KEY,
    kind            text        NOT NULL,
    payload         jsonb       NOT NULL,
    attempts        smallint    NOT NULL DEFAULT 0,
    max_attempts    smallint    NOT NULL DEFAULT 5,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    failed_at       timestamptz,
    last_error      varchar(512),
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS outbox_pending_idx
    ON outbox (next_attempt_at) WHERE failed_at IS NULL;
CREATE INDEX IF NOT EXISTS outbox_failed_idx
    ON outbox (failed_at) WHERE failed_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS outbox;
-- +goose StatementEnd
