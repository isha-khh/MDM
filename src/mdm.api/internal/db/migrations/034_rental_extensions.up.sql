-- 續借（延長預計歸還日）：借用中的租借可以申請延長，經保管人/管理員審核
-- 核准後才會真的更新 rentals.expected_return。
--
-- 以 rental_number（整批）為單位，跟 rentals 的其他批次操作一致。申請當下
-- 的原預計歸還日另外存一份（previous_expected_return），核准後 rentals 上的
-- 日期會被覆蓋，之後仍可追溯「從哪天延到哪天」。
CREATE TABLE IF NOT EXISTS rental_extensions (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rental_number             INTEGER NOT NULL,
    requested_by              UUID REFERENCES users(id),
    previous_expected_return  DATE NOT NULL,
    requested_expected_return DATE NOT NULL,
    reason                    TEXT NOT NULL,
    status                    TEXT NOT NULL DEFAULT 'pending', -- pending | approved | rejected
    decided_by                UUID REFERENCES users(id),
    decided_at                TIMESTAMPTZ,
    decision_note             TEXT NOT NULL DEFAULT '',
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_rental_extensions_number ON rental_extensions(rental_number);

-- 同一批租借同時只能有一筆審核中的續借申請；審核完才能再申請下一次。
CREATE UNIQUE INDEX IF NOT EXISTS uniq_rental_extensions_pending
    ON rental_extensions(rental_number) WHERE status = 'pending';
