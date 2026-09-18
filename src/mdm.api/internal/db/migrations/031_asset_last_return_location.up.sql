-- Phase 2c of 租借 2.1 改版: 資產「最後歸還位置」，與 assets.location
-- （管理者手動維護的存放地點）分開、不互相覆蓋。第二階段核對完成時，若
-- 最終 checklist 裡有 location 型別的項目，就寫回這兩欄，下次有人在
-- AssetPicker 挑選這台資產租借時可以看到。詳見 docs/租借模組改版規劃.md。
ALTER TABLE assets ADD COLUMN IF NOT EXISTS last_return_location JSONB; -- {"lat":..,"lng":..,"accuracy":..} 或 {"address":".."}
ALTER TABLE assets ADD COLUMN IF NOT EXISTS last_return_at       TIMESTAMPTZ;
