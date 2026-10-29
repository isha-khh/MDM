-- 租借占用的「時間區間」。
--
-- 以前租借只有日期（borrow_date、expected_return）加上整天／上午／下午，判定
-- 「這個資產被借走了嗎」時只要有任何進行中的租借就算借出，不看日期。為了接受
-- 帶時間的預約（例如派發系統預約公司車 09:00-12:00），改成比較時間區間是否重疊：
--
--   * start_at / end_at：選填；帶時間的預約才有，精度到分鐘。
--   * 沒有 start_at / end_at 的舊租借，區間由日期與時段推算（台灣時間）：
--       整天 [借出日 00:00, 預計歸還日隔天 00:00)
--       上午 [借出日 00:00, 借出日 12:00)     下午 [借出日 12:00, 隔天 00:00)
--       沒有預計歸還日 = 沒有結束時間（一直占用）
--   * 借出中／待確認歸還的租借，結束時間至少延到一分鐘後（車還在外面，逾期也一樣占用；
--     加一分鐘是因為區間不含結束點，否則「現在」會剛好落在區間外）。
-- 預約功能：依「分類」開啟（沿用 category_rental_rules 與分類繼承邏輯）。開啟的分類，
-- 資產即使目前借出中也能預約之後的時段，只要不與其他租借的時間區間重疊；沒開的分類
-- 維持原本行為（資產有任何進行中的租借就不能借）。
ALTER TABLE category_rental_rules ADD COLUMN IF NOT EXISTS reservation_enabled BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE rentals ADD COLUMN IF NOT EXISTS start_at TIMESTAMPTZ;
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS end_at   TIMESTAMPTZ;

CREATE OR REPLACE FUNCTION rental_span(r rentals) RETURNS tstzrange AS $$
DECLARE
    s timestamptz;
    e timestamptz;
    d date;
BEGIN
    IF r.start_at IS NOT NULL AND r.end_at IS NOT NULL THEN
        s := r.start_at;
        e := r.end_at;
    ELSE
        -- borrow_date 是以 UTC 午夜存的日期（沿用既有判定方式）。
        d := (r.borrow_date AT TIME ZONE 'UTC')::date;
        s := ((d + CASE WHEN r.period = 'pm' THEN time '12:00' ELSE time '00:00' END)::timestamp) AT TIME ZONE 'Asia/Taipei';
        IF r.expected_return IS NULL THEN
            e := 'infinity';
        ELSIF r.period = 'am' THEN
            e := ((r.expected_return + time '12:00')::timestamp) AT TIME ZONE 'Asia/Taipei';
        ELSE
            e := ((r.expected_return + 1)::timestamp) AT TIME ZONE 'Asia/Taipei';
        END IF;
    END IF;
    IF r.status IN ('active', 'pending_return') THEN
        e := GREATEST(e, now() + interval '1 minute');
    END IF;
    IF e < s THEN
        e := s; -- 資料異常（歸還日早於借出日）：視為空區間，不要讓查詢出錯
    END IF;
    RETURN tstzrange(s, e, '[)');
END
$$ LANGUAGE plpgsql STABLE;

CREATE INDEX IF NOT EXISTS idx_rentals_asset_status ON rentals(asset_id, status);
