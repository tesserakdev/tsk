CREATE TABLE IF NOT EXISTS credential_rotations (
    id   TEXT     PRIMARY KEY,
    keys TEXT     NOT NULL DEFAULT '',
    ts   DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_credential_rotations_ts ON credential_rotations (ts);
