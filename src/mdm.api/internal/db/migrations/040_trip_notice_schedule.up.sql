-- Daily automatic sending of the business-trip rental-car notices. Single row.
-- Off until an admin turns it on. last_run_date (Taipei date, 'YYYY-MM-DD') is
-- the day's claim: the scheduler takes it with one conditional UPDATE, so even
-- with several server instances only one of them runs that day.
CREATE TABLE IF NOT EXISTS trip_notice_schedule (
    id               TEXT        PRIMARY KEY DEFAULT 'default',
    enabled          BOOLEAN     NOT NULL DEFAULT FALSE,
    send_hour        INTEGER     NOT NULL DEFAULT 9 CHECK (send_hour BETWEEN 0 AND 23),
    last_run_date    TEXT        NOT NULL DEFAULT '',
    last_run_at      TIMESTAMPTZ,
    last_run_summary TEXT        NOT NULL DEFAULT '',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       TEXT
);

INSERT INTO trip_notice_schedule (id) VALUES ('default') ON CONFLICT DO NOTHING;
