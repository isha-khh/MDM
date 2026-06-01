-- Add supported platforms to managed_apps. Stored as a comma-separated string
-- of lower-case platform tokens (ios,ipados,macos,tvos,watchos). The UI uses
-- this both for App Store search-filter and for hiding install buttons on
-- devices whose platform isn't covered. Default reflects the historical case
-- where every catalogued app was assumed to be an iOS/iPadOS app.

ALTER TABLE managed_apps
  ADD COLUMN IF NOT EXISTS supported_platforms TEXT NOT NULL DEFAULT 'ios,ipados';
