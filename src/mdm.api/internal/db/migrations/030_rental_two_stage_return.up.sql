-- Phase 2b of 租借 2.1 改版: 歸還改為兩階段流程。
-- status 沿用既有 TEXT 欄位（無 CHECK 限制），新增一個中間值 'pending_return'：
--   active -> pending_return（借用人/管理員回報後）-> returned（保管人/管理員核對後）
-- 詳見 docs/租借模組改版規劃.md。
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS return_checklist_reported JSONB;   -- 借用人回報的原始版本
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS return_reported_by UUID REFERENCES users(id);
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS return_reported_at TIMESTAMPTZ;
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS return_verified_by UUID REFERENCES users(id);
-- 既有的 return_checklist / return_notes / actual_return 語意不變，但改成
-- 「保管人/管理員核對後的最終版本」，在第二階段（核對）才寫入。

-- 逐日追蹤分類：核對時若發現「實際歸還日期（今天）」晚於「預計歸還日期」
-- （不論原單是單日還是跨日訂單），不硬擋，但強制要求填寫這欄說明原因。
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS cross_day_reason TEXT NOT NULL DEFAULT '';
