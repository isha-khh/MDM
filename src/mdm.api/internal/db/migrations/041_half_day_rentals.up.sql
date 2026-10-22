-- 半天出借：
--   1. 分類可開啟「允許半天出借」（沿用 category_rental_rules 與分類繼承邏輯）。
--   2. 租借單記錄時段：full = 整天（預設，行為與舊版完全相同）、am = 上午、
--      pm = 下午。半天租借限單日（借出日 = 預計歸還日），同一資產同一天上午、
--      下午可由不同人各借一段。
ALTER TABLE category_rental_rules ADD COLUMN IF NOT EXISTS half_day_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE rentals ADD COLUMN IF NOT EXISTS period TEXT NOT NULL DEFAULT 'full';
