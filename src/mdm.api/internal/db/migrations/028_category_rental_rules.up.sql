-- Phase 1 of 租借 2.1 改版:
--   1. 借出日期在建立時可以填寫（原本只能靠 DEFAULT now()）。
--   2. 車輛等特定分類可以標記為「逐日追蹤」：跨日租借需要說明原因，且租借
--      期間每天各自要有一份 checklist 回報（用於記錄每日里程等資訊）。
-- 詳細設計見 docs/租借模組改版規劃.md。

-- 建立跨日租借時的必填說明（跟歸還時的 cross_day_reason 是不同時機的欄位，
-- 後者留給 Phase 2b 的兩階段歸還一起加）。
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS multi_day_reason TEXT NOT NULL DEFAULT '';

-- 分類是否需要「逐日追蹤」，與之後 Phase 2 的 checklist_templates /
-- category_notices 共用同一套「沿分類樹往上找第一個有明確設定的祖先」的
-- 繼承查找邏輯（沒有任何祖先設定過 = 不套用）。
CREATE TABLE IF NOT EXISTS category_rental_rules (
    category_id             UUID PRIMARY KEY REFERENCES categories(id) ON DELETE CASCADE,
    daily_tracking_required BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by              UUID REFERENCES users(id),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 逐日回報：daily_tracking_required 分類的租借，借用人（或管理員）每天各填
-- 一份，跟「歸還」是兩個分開的動作——回報不影響 rentals.status。用
-- rental_number（而非單一 rentals.id）鍵入，因為租借操作一律以整批
-- （同一 rental_number）為單位，比照 ReturnByNumber/ActivateByNumber 等既有慣例。
CREATE TABLE IF NOT EXISTS rental_daily_reports (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rental_number   INTEGER NOT NULL,
    report_date     DATE NOT NULL,
    checklist       JSONB NOT NULL,
    backfill_reason TEXT NOT NULL DEFAULT '',  -- 非空 = 事後補登（忘記當天填），供稽核標記
    reported_by     UUID REFERENCES users(id),
    reported_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (rental_number, report_date)
);
CREATE INDEX IF NOT EXISTS idx_rental_daily_reports_number ON rental_daily_reports(rental_number);
