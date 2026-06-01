package domain

import "time"

type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string // "admin", "operator", "viewer" (legacy)
	SystemRole   string // "sys_admin", "user"
	Email        string
	DisplayName  string
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Device struct {
	UDID             string
	SerialNumber     string
	DeviceName       string
	Model            string
	OSVersion        string
	LastSeen         time.Time
	EnrollmentStatus string
	IsSupervised     bool
	IsLostMode       bool
	BatteryLevel     float64
	Details          map[string]interface{} // cached command results (apps, profiles, security, etc.)
}

type AuditLog struct {
	ID        string
	UserID    string
	Username  string
	Action    string
	Target    string
	Detail    string
	Module    string // "system", "asset", "mdm", "rental"
	IPAddress string
	UserAgent string
	Timestamp time.Time
}

type MDMEvent struct {
	ID          string
	EventType   string // "acknowledge", "checkin"
	UDID        string
	CommandUUID string
	Status      string
	RawPayload  string
	Timestamp   time.Time
}

type CommandResult struct {
	CommandUUID string
	StatusCode  int
	RawResponse string
}

// DEPAssignment tracks that a serial has had a DEP profile applied by the
// auto-assigner. One row per serial; re-applying updates the row.
type DEPAssignment struct {
	SerialNumber   string
	ProductFamily  string // as reported by ABM: Mac / iPad / iPhone / AppleTV
	TemplateFamily string // which template file was used (lowercased family)
	ProfileUUID    string // Apple-returned DEP profile UUID
	AppliedAt      time.Time
	LastError      string
}

// ABMDevice is one device as returned by Apple Business Manager's
// /v1/orgDevices endpoint.
type ABMDevice struct {
	Serial        string
	DeviceModel   string
	ProductFamily string // Mac / iPad / iPhone / AppleTV
	Status        string // ASSIGNED / ...
	AddedToOrg    time.Time
}

// --- Asset Management ---

type Asset struct {
	ID            string
	DeviceUdid    *string
	AssetNumber   string
	Name          string
	Spec          string
	Quantity      int
	Unit          string
	AcquiredDate  *time.Time
	UnitPrice     float64
	Purpose       string
	AssignedDate  *time.Time // 保管人領用日期（原 borrow_date）
	CustodianID   *string    // 保管人（長期負責人），僅透過 custody API 變更
	CustodianName string
	Location      string
	AssetCategory string
	Notes         string
	CategoryID    *string
	AssetStatus   string
	// Current holder (temporary holder via rental; custodian stays fixed)
	CurrentHolderID    *string
	CurrentHolderName  string
	CurrentHolderSince *time.Time
	// Lifecycle fields
	DisposedAt    *time.Time
	DisposedBy    *string
	DisposeReason string
	TransferredTo string
	TransferredAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// Joined fields (read-only)
	DeviceName   string
	DeviceSerial string
	CategoryName string
}

// AssetCustodyLog records every change to an asset's custodian.
// Append-only audit trail for ISO 27001 A.8 compliance.
type AssetCustodyLog struct {
	ID           string
	AssetID      string
	Action       string // "assign", "transfer", "revoke"
	FromUserID   *string
	FromUserName string
	ToUserID     *string
	ToUserName   string
	Reason       string
	OperatedBy   *string
	OperatorName string
	CreatedAt    time.Time
}

// --- Inventory / Stocktaking ---

type InventorySession struct {
	ID           string
	Name         string
	Description  string
	Status       string // "draft", "in_progress", "completed"
	CreatedBy    string
	CreatorName  string
	CreatedAt    time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
	Notes        string
	TotalCount   int
	CheckedCount int
	MatchedCount int
	MissingCount int
}

type InventoryItem struct {
	ID          string
	SessionID   string
	AssetID     string
	DeviceUdid  string
	AssetNumber string
	AssetName   string
	Found       *bool  // nil = not checked, true = found, false = missing
	Condition   string // "good", "damaged", "other", ""
	CheckedBy   *string
	CheckerName string
	CheckedAt   *time.Time
	Notes       string
}

// --- Rental Management ---

type Rental struct {
	ID              string
	AssetID         *string // primary link (may be nil for legacy rows)
	DeviceUdid      string  // nullable in DB; empty string for standalone assets
	BorrowerID      string
	BorrowerName    string
	ApproverID      *string
	ApproverName    string
	CustodianID     *string
	CustodianName   string
	Status          string // pending, approved, active, returned, rejected
	Purpose         string
	BorrowDate      time.Time
	ExpectedReturn  *time.Time
	ActualReturn    *time.Time
	Notes           string
	RentalNumber    int
	IsArchived      bool
	ReturnChecklist map[string]interface{}
	ReturnNotes     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	// Joined fields (read-only)
	DeviceName   string
	DeviceSerial string
	AssetNumber  string
	AssetName    string
}

// --- App Management ---

type ManagedApp struct {
	ID            string
	Name          string
	BundleID      string
	AppType       string // "vpp", "enterprise"
	ItunesStoreID string
	ManifestURL   string
	PurchasedQty  int
	Notes         string
	IconURL       string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// Computed
	InstalledCount int
}

type DeviceApp struct {
	ID          string
	DeviceUdid  string
	AppID       string
	InstalledAt time.Time
	// Joined fields
	AppName  string
	BundleID string
	AppType  string
}

type PendingAppCommand struct {
	CommandUUID string
	Action      string // "install", "uninstall"
	DeviceUdid  string
	AppID       string
}

// --- Category ---

type Category struct {
	ID        string
	ParentID  *string
	Name      string
	Level     int
	SortOrder int
	CreatedAt time.Time
}

// --- Profile ---

type Profile struct {
	ID         string
	Name       string
	Filename   string
	Content    []byte
	Size       int
	UploadedBy string
	CreatedAt  time.Time
}

// --- Module Permission ---

type ModulePermission struct {
	ID         string
	UserID     string
	Module     string // "asset", "mdm", "rental"
	Permission string // "viewer", "operator", "manager", "requester", "approver"
	GrantedBy  *string
	GrantedAt  time.Time
}

// --- Notification ---

type Notification struct {
	ID           string
	Type         string // "email"
	Event        string // "rental_request", "rental_approved", etc.
	Recipient    string // email address
	Subject      string
	Body         string
	Status       string // "pending", "sent", "failed"
	ErrorMessage string
	ReferenceID  string
	CreatedAt    time.Time
	SentAt       *time.Time
}

// --- Device List View (joined query) ---

type DeviceListItem struct {
	UDID             string
	SerialNumber     string
	DeviceName       string
	Model            string
	OSVersion        string
	LastSeen         time.Time
	EnrollmentStatus string
	IsSupervised     bool
	IsLostMode       bool
	BatteryLevel     float64
	CustodianName    string
	CategoryName     string
	CategoryID       *string
	CustodianID      *string
	AssetStatus      string
}

// MailSettings is the single-row configuration for outgoing + incoming mail.
type MailSettings struct {
	// Outgoing (SMTP)
	SMTPEnabled  bool
	SMTPHost     string
	SMTPPort     string
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPFromName string
	SMTPTLS      bool

	// Incoming (IMAP / POP3)
	IncomingEnabled  bool
	IncomingProtocol string // "imap" | "pop3"
	IncomingHost     string
	IncomingPort     string
	IncomingUsername string
	IncomingPassword string
	IncomingTLS      bool
	IncomingMailbox  string

	UpdatedAt time.Time
	UpdatedBy *string
}
