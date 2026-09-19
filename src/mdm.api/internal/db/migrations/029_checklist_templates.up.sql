-- Phase 2a of 租借 2.1 改版: dynamic, category-bound return checklist
-- templates, replacing the previously hardcoded 5-item checklist. See
-- docs/租借模組改版規劃.md for the full design.

-- items is a JSON array of {key,label,type,required,unit?,maxCount?}.
-- category_id NULL is the global default/fallback template (originally the
-- hardcoded 5 boolean items). A category with no row of its own inherits
-- from its nearest ancestor that has one, falling back to the NULL row —
-- same "walk up categories.parent_id" resolution as category_rental_rules.
CREATE TABLE IF NOT EXISTS checklist_templates (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id UUID UNIQUE REFERENCES categories(id) ON DELETE CASCADE,
    items       JSONB NOT NULL,
    updated_by  UUID REFERENCES users(id),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Photos uploaded for a "photo" type checklist item. Content lives directly
-- in the database (bytea), same convention as the existing mobileconfig
-- profile uploads — no separate object-storage service. Keyed by
-- rental_number (not a single rentals.id row) because return_checklist
-- itself is already replicated identically across every row sharing a
-- rental_number (see ReturnByNumber), so a photo answer is a batch-level
-- answer too, not a per-asset-row one.
CREATE TABLE IF NOT EXISTS checklist_photos (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rental_number INTEGER NOT NULL,
    item_key      TEXT NOT NULL,
    content       BYTEA NOT NULL,
    content_type  TEXT NOT NULL,
    size          INTEGER NOT NULL,
    uploaded_by   UUID REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_checklist_photos_rental_number ON checklist_photos(rental_number);

-- Seed the global default template = the 5 items that used to be hardcoded
-- in Rentals.tsx, so existing behavior is preserved for every category that
-- doesn't have (or inherit) a more specific template.
INSERT INTO checklist_templates (category_id, items) VALUES (NULL, '[
  {"key":"deviceReceived","label":"已收到裝置","type":"boolean","required":true},
  {"key":"screenOk","label":"螢幕完好（無刮傷、裂痕）","type":"boolean","required":true},
  {"key":"bodyOk","label":"機身完好（無凹損、變形）","type":"boolean","required":true},
  {"key":"canPowerOn","label":"可正常開機使用","type":"boolean","required":true},
  {"key":"accessoriesOk","label":"配件齊全（充電線、保護套等）","type":"boolean","required":true}
]'::jsonb) ON CONFLICT DO NOTHING;
