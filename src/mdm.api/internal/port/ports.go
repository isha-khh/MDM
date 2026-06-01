package port

import (
	"context"
	"time"

	"github.com/anthropics/mdm-server/internal/domain"
)

// UserRepository persists users.
type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	GetByID(ctx context.Context, id string) (*domain.User, error)
	GetByUsername(ctx context.Context, username string) (*domain.User, error)
	List(ctx context.Context) ([]*domain.User, error)
	Update(ctx context.Context, user *domain.User) error
	Delete(ctx context.Context, id string) error
}

// DeviceRepository persists devices.
type DeviceRepository interface {
	Upsert(ctx context.Context, device *domain.Device) error
	GetByUDID(ctx context.Context, udid string) (*domain.Device, error)
	List(ctx context.Context, filter string, limit int, offset int) ([]*domain.Device, int, error)
	SetLostMode(ctx context.Context, udid string, enabled bool) error
}

// AuditRepository persists audit logs.
type AuditRepository interface {
	Create(ctx context.Context, log *domain.AuditLog) error
	List(ctx context.Context, userID string, action string, module string, limit int, offset int) ([]*domain.AuditLog, error)
}

// MicroMDMClient calls the MicroMDM HTTP API.
type MicroMDMClient interface {
	ListDevices(ctx context.Context) ([]*domain.Device, error)
	GetDevice(ctx context.Context, udid string) (*domain.Device, error)
	SendCommand(ctx context.Context, payload map[string]interface{}) (*domain.CommandResult, error)
	SendPush(ctx context.Context, udid string) error
	ClearQueue(ctx context.Context, udid string) (*domain.CommandResult, error)
	InspectQueue(ctx context.Context, udid string) (string, error)
	SyncDEP(ctx context.Context) error
	DefineDEPProfile(ctx context.Context, template map[string]interface{}, serials []string) (string, error)
}

// VPPClient calls the Apple VPP API.
type VPPClient interface {
	AssignLicense(ctx context.Context, adamID string, serialNumbers []string) (string, error)
	RevokeLicense(ctx context.Context, adamID string, serialNumbers []string) (string, error)
}

// AssetRepository persists assets.
type AssetRepository interface {
	IsCustodianOfAll(ctx context.Context, userID string, udids []string) (bool, error)
	List(ctx context.Context, deviceUdid string) ([]*domain.Asset, error)
	GetByID(ctx context.Context, id string) (*domain.Asset, error)
	GetByDeviceUdid(ctx context.Context, udid string) (*domain.Asset, error)
	Create(ctx context.Context, asset *domain.Asset) (string, error)
	Update(ctx context.Context, id string, fields map[string]interface{}) error
	Delete(ctx context.Context, id string) error
	UpdateStatus(ctx context.Context, udid string, status string) error
	Dispose(ctx context.Context, id string, disposedBy string, reason string) error
	Transfer(ctx context.Context, id string, transferredTo string) error

	// Custody operations — replace direct custodian writes.
	SetCustodian(ctx context.Context, id string, custodianID *string, custodianName string, assignedDate *time.Time) error
	SetHolderByUdid(ctx context.Context, udid string, holderID string, holderName string) error
	ClearHolderByUdid(ctx context.Context, udid string) error
}

// CustodyRepository persists the asset custody audit trail.
type CustodyRepository interface {
	Append(ctx context.Context, log *domain.AssetCustodyLog) error
	ListByAsset(ctx context.Context, assetID string) ([]*domain.AssetCustodyLog, error)
}

// InventoryRepository persists inventory sessions and items.
type InventoryRepository interface {
	// Sessions
	CreateSession(ctx context.Context, session *domain.InventorySession) (string, error)
	GetSession(ctx context.Context, id string) (*domain.InventorySession, error)
	ListSessions(ctx context.Context) ([]*domain.InventorySession, error)
	UpdateSessionStatus(ctx context.Context, id string, status string) error
	UpdateSessionNotes(ctx context.Context, id string, notes string) error
	DeleteSession(ctx context.Context, id string) error
	UpdateSessionCounts(ctx context.Context, id string) error

	// Items
	GenerateItems(ctx context.Context, sessionID string) (int, error)
	ListItems(ctx context.Context, sessionID string) ([]*domain.InventoryItem, error)
	CheckItem(ctx context.Context, id string, found bool, condition string, checkedBy string, checkerName string, notes string) error
}

// RentalRepository persists rentals.
type RentalRepository interface {
	List(ctx context.Context, status, deviceUdid string, showArchived bool) ([]*domain.Rental, error)
	Create(ctx context.Context, rental *domain.Rental) (string, error)
	GetByID(ctx context.Context, id string) (*domain.Rental, error)
	NextRentalNumber(ctx context.Context) (int, error)
	UpdateStatusByNumber(ctx context.Context, rentalNumber int, fromStatus, toStatus string, approverID *string, approverName string) error
	ActivateByNumber(ctx context.Context, rentalNumber int) error
	ReturnByNumber(ctx context.Context, rentalNumber int, checklist []byte, notes string) error
	DeleteByNumber(ctx context.Context, rentalNumber int) error
	Archive(ctx context.Context, ids []string) error
	ListDeviceUdidsByNumber(ctx context.Context, rentalNumber int) ([]string, error)
	GetBorrowerInfo(ctx context.Context, rentalID string) (borrowerID, borrowerName string, err error)
	CheckDeviceAvailability(ctx context.Context, udid string) (assetStatus string, isRented bool, isLostMode bool, err error)
	ListOverdue(ctx context.Context) ([]*domain.Rental, error)
}

// AppRepository persists managed apps and device-app bindings.
type AppRepository interface {
	ListManagedApps(ctx context.Context) ([]*domain.ManagedApp, error)
	GetManagedApp(ctx context.Context, id string) (*domain.ManagedApp, error)
	CreateManagedApp(ctx context.Context, app *domain.ManagedApp) (string, error)
	UpdateManagedApp(ctx context.Context, id string, fields map[string]interface{}) error
	DeleteManagedApp(ctx context.Context, id string) error
	ListDeviceApps(ctx context.Context, deviceUdid string) ([]*domain.DeviceApp, error)
	InstalledCount(ctx context.Context, appID string) (int, error)
	IsInstalledOn(ctx context.Context, deviceUdid, appID string) (bool, error)
	CreatePendingCommand(ctx context.Context, cmd *domain.PendingAppCommand) error
	DeletePendingCommand(ctx context.Context, commandUUID string) error
	GetPendingCommand(ctx context.Context, commandUUID string) (*domain.PendingAppCommand, error)
	AddDeviceApp(ctx context.Context, deviceUdid, appID string) error
	RemoveDeviceApp(ctx context.Context, deviceUdid, appID string) error
	ListBundleMap(ctx context.Context) (map[string]string, error)
	SyncDeviceApp(ctx context.Context, deviceUdid, appID string) (bool, error)
	ListAppsNeedingIcons(ctx context.Context) ([]struct{ ID, BundleID, ItunesID string }, error)
	UpdateIcon(ctx context.Context, id, iconURL, itunesID string) error
}

// CategoryRepository persists categories.
type CategoryRepository interface {
	List(ctx context.Context) ([]*domain.Category, error)
	Create(ctx context.Context, cat *domain.Category) (string, error)
	GetLevel(ctx context.Context, id string) (int, error)
	Update(ctx context.Context, id string, name string) error
	Delete(ctx context.Context, id string) error
}

// ProfileRepository persists mobileconfig profiles.
type ProfileRepository interface {
	List(ctx context.Context) ([]*domain.Profile, error)
	Create(ctx context.Context, profile *domain.Profile) (string, error)
	GetContent(ctx context.Context, id string) (content []byte, filename string, err error)
	Delete(ctx context.Context, id string) error
}

// PermissionRepository persists module-level permissions.
type PermissionRepository interface {
	GetByUserID(ctx context.Context, userID string) ([]*domain.ModulePermission, error)
	GetByUserAndModule(ctx context.Context, userID string, module string) (*domain.ModulePermission, error)
	Set(ctx context.Context, perm *domain.ModulePermission) error
	Delete(ctx context.Context, userID string, module string) error
}

// NotificationRepository persists notification records.
type NotificationRepository interface {
	Create(ctx context.Context, notif *domain.Notification) (string, error)
	UpdateStatus(ctx context.Context, id string, status string, errMsg string) error
	List(ctx context.Context, event string, referenceID string, limit int) ([]*domain.Notification, error)
}

// EmailSender sends email via SMTP.
type EmailSender interface {
	Send(ctx context.Context, to string, subject string, htmlBody string) error
}

// MailSettingsRepository persists the mail server configuration (single row).
type MailSettingsRepository interface {
	Get(ctx context.Context) (*domain.MailSettings, error)
	Upsert(ctx context.Context, settings *domain.MailSettings, updatedBy string) error
}

// EventBroker fans out MDM events to subscribers.
type EventBroker interface {
	Publish(event *domain.MDMEvent)
	Subscribe(ctx context.Context) <-chan *domain.MDMEvent
}

// ABMClient calls the Apple School and Business Manager API.
type ABMClient interface {
	ListOrgDevices(ctx context.Context) ([]domain.ABMDevice, error)
}

// DEPAssignmentRepo persists which serials have had a DEP profile applied.
type DEPAssignmentRepo interface {
	Get(ctx context.Context, serial string) (*domain.DEPAssignment, error) // returns nil, nil when absent
	Upsert(ctx context.Context, a *domain.DEPAssignment) error
	ListSerials(ctx context.Context) (map[string]bool, error) // set of all known serials, for batch diff
}
