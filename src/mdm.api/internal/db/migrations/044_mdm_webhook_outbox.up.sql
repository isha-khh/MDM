-- MDM → 派發系統的預約異動通知（規格 §7.6）的待送佇列。
--
-- 行政人員在 MDM 網頁上建立、修改、取消預約（或出車、還車）時，把事件先寫進這裡，
-- 由背景工作送出；送不出去就依 1、2、4、8、16 分鐘重試，共 5 次。
-- body 存的是實際送出的原始 JSON：重送時內容與 eventId 都不變，簽章以它計算。
-- 同一筆預約的事件依 id 順序送，前一個還沒送完（pending）時後一個等著。
CREATE TABLE IF NOT EXISTS integration_webhook_outbox (
    id              BIGSERIAL PRIMARY KEY,
    event_id        TEXT NOT NULL UNIQUE,
    event_type      TEXT NOT NULL,
    reservation_id  TEXT NOT NULL,
    body            BYTEA NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',   -- pending, sent, failed
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_webhook_outbox_due ON integration_webhook_outbox(status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_webhook_outbox_reservation ON integration_webhook_outbox(reservation_id, id);
