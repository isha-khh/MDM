-- 派發任務系統（dispatch）預約整合。
--
-- 派發系統透過獨立連接埠的 API（/api/v1/reservations …）預約公司租賃車輛。預約沿用
-- rentals，所以網頁上看到的就是同一份資料；這裡只補 API 需要的欄位與三張表：
--
--   * rentals.external_ref：呼叫端的單號（來源:單號，如 protrack:P-88）。未取消的預約之間不可重複。
--   * rentals.created_via：'web'（網頁）或 'api'（整合 API），之後 webhook 用來避免回報自己造成的異動。
--   * rentals.borrower_ref：API 帶進來的借用人識別（AD 帳號），原樣回傳給呼叫端。
--   * 'cancelled' 狀態：取消為狀態變更，不實體刪除（rentals.status 本來就沒有 CHECK）。
--   * integration_clients：API key（只存雜湊）、IP 白名單、可自動建立借用人帳號的信箱網域。
--   * integration_resources：把對外的資源代號（RENTAL_CAR_01）對到 MDM 的資產，並設定可預約的縣市。
--   * integration_idempotency：Idempotency-Key 的重送結果。
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS external_ref  TEXT;
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS created_via   TEXT NOT NULL DEFAULT 'web';
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS borrower_ref  TEXT;
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS passengers    TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS destination   JSONB;
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS cancel_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS cancelled_at  TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS ux_rentals_external_ref
    ON rentals(external_ref) WHERE external_ref IS NOT NULL AND status <> 'cancelled';

CREATE TABLE IF NOT EXISTS integration_clients (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                      TEXT NOT NULL,
    key_hash                  TEXT NOT NULL UNIQUE,           -- SHA-256(hex) of the API key; the key itself is never stored
    key_prefix                TEXT NOT NULL DEFAULT '',       -- first characters of the key, to recognise it in the UI
    enabled                   BOOLEAN NOT NULL DEFAULT TRUE,
    ip_allowlist              TEXT[] NOT NULL DEFAULT '{}',   -- IPs / CIDRs; empty = any address
    auto_create_email_domains TEXT[] NOT NULL DEFAULT '{mail.isha.org.tw}', -- borrower accounts may be created for these domains; empty = never
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at                TIMESTAMPTZ,
    last_used_at              TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS integration_resources (
    resource_id    TEXT PRIMARY KEY,
    service_type   TEXT NOT NULL DEFAULT 'RENTAL_CAR',
    asset_id       UUID NOT NULL REFERENCES assets(id),
    allowed_cities TEXT[] NOT NULL DEFAULT '{台南市,高雄市,屏東縣}', -- destination.city must be one of these; empty = no restriction
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS integration_idempotency (
    client_id    UUID NOT NULL REFERENCES integration_clients(id) ON DELETE CASCADE,
    idem_key     TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status_code  INTEGER NOT NULL,
    response     BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (client_id, idem_key)
);
CREATE INDEX IF NOT EXISTS idx_integration_idempotency_created ON integration_idempotency(created_at);
