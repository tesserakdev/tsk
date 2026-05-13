CREATE TABLE IF NOT EXISTS requests (
    id            TEXT     PRIMARY KEY,
    tool          TEXT     NOT NULL,
    params        TEXT     NOT NULL DEFAULT '',
    status        INTEGER  NOT NULL DEFAULT 0,
    scrub_actions INTEGER  NOT NULL DEFAULT 0,
    ts            DATETIME NOT NULL,
    response      TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_requests_tool ON requests (tool);
CREATE INDEX IF NOT EXISTS idx_requests_ts   ON requests (ts);
