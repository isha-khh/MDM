-- DEP profile assignments tracked per device serial.
-- The scheduler pulls ABM's full device list and applies a DEP profile
-- (mac.json / ipad.json / iphone.json) to any serial NOT in this table.
-- One row per serial; re-applying overwrites the latest profile_uuid.

CREATE TABLE IF NOT EXISTS dep_assignments (
    serial_number   TEXT PRIMARY KEY,
    product_family  TEXT NOT NULL DEFAULT '',  -- as reported by ABM: Mac / iPad / iPhone / AppleTV
    template_family TEXT NOT NULL DEFAULT '',  -- which template file was used (lowercased)
    profile_uuid    TEXT NOT NULL DEFAULT '',  -- Apple-returned DEP profile UUID (changes each apply)
    applied_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_dep_assignments_family ON dep_assignments(template_family);
