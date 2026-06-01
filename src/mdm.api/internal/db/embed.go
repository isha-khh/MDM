package db

import _ "embed"

//go:embed migrations/001_init.up.sql
var MigrationSQL string

//go:embed migrations/002_device_details.up.sql
var Migration002SQL string

//go:embed migrations/003_assets.up.sql
var Migration003SQL string

//go:embed migrations/004_rentals.up.sql
var Migration004SQL string

//go:embed migrations/005_categories.up.sql
var Migration005SQL string

//go:embed migrations/006_user_active.up.sql
var Migration006SQL string

//go:embed migrations/007_managed_apps.up.sql
var Migration007SQL string

//go:embed migrations/008_rental_archive.up.sql
var Migration008SQL string

//go:embed migrations/009_rental_batch_fix.up.sql
var Migration009SQL string

//go:embed migrations/010_rental_checklist.up.sql
var Migration010SQL string

//go:embed migrations/011_app_icon.up.sql
var Migration011SQL string

//go:embed migrations/012_asset_status.up.sql
var Migration012SQL string

//go:embed migrations/013_pending_app_commands.up.sql
var Migration013SQL string

//go:embed migrations/014_module_permissions.up.sql
var Migration014SQL string

//go:embed migrations/015_asset_lifecycle_inventory.up.sql
var Migration015SQL string

//go:embed migrations/016_custody_redesign.up.sql
var Migration016SQL string

//go:embed migrations/017_mail_settings.up.sql
var Migration017SQL string

//go:embed migrations/018_rental_asset_id.up.sql
var Migration018SQL string

//go:embed migrations/019_dep_assignments.up.sql
var Migration019SQL string
