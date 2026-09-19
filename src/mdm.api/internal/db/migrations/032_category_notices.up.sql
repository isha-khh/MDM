-- Phase 3 of 租借 2.1 改版: per-category rental "notice" (使用須知/注意事項)
-- that a borrower must read and acknowledge before their rental request can
-- be submitted. See docs/租借模組改版規劃.md for the full design.

-- Same "walk up categories.parent_id, first explicit row wins" inheritance
-- as category_rental_rules/checklist_templates, but with no global-default
-- fallback: a category with no notice anywhere in its ancestor chain simply
-- has nothing to show (most categories don't need one).
CREATE TABLE IF NOT EXISTS category_notices (
    category_id UUID PRIMARY KEY REFERENCES categories(id) ON DELETE CASCADE,
    content     TEXT NOT NULL,
    updated_by  UUID REFERENCES users(id),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per (user, notice category) they've agreed to. content_hash lets
-- resolve tell "already agreed, content unchanged since" apart from "agreed
-- to a since-edited version" (the latter must be shown again) without
-- storing the full text a second time.
CREATE TABLE IF NOT EXISTS category_notice_acks (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id),
    category_id   UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    content_hash  TEXT NOT NULL,
    acked_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, category_id)
);
