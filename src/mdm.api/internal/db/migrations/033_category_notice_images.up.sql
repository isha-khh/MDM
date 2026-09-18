-- Images embedded inline (via <img src="/api/category-notice-images/{id}">)
-- in a category_notices.content rich-text body. Content lives directly in
-- the database (bytea), same convention as checklist_photos and the
-- existing mobileconfig profile uploads — no separate object-storage
-- service. category_id is nullable/not enforced against a specific notice
-- row so uploads can happen mid-edit (before the notice text itself is
-- saved) without a foreign key ordering problem.
CREATE TABLE IF NOT EXISTS category_notice_images (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id  UUID REFERENCES categories(id) ON DELETE CASCADE,
    content      BYTEA NOT NULL,
    content_type TEXT NOT NULL,
    size         INTEGER NOT NULL,
    uploaded_by  UUID REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
