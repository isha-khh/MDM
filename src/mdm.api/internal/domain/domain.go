package domain

import "time"

type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string // "admin", "operator", "viewer" (legacy)
	SystemRole   string // "sys_admin", "user"
	Email        string
	SSOSub       string // OIDC subject identifier; empty = not linked
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

// VPPAsset mirrors one entry from Apple's getVPPAssetsSrv response — the unit
// of VPP licensing. AdamID is the iTunes Store track id; the qty fields tell
// us how many licenses are purchased / in use, used to sync purchased_qty.
type VPPAsset struct {
	AdamID          string
	ProductTypeName string // "Software" / "Publication"
	TotalCount      int    // total licenses purchased in ABM
	AssignedCount   int    // currently assigned to devices/users
	RetiredCount    int    // revoked but not yet re-usable
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
	IsRentable    bool
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

// PickableAsset is the lightweight shape returned to asset-picker UIs
// (maintenance/disposal requests, etc.) that need every asset regardless of
// its IsRentable flag. Covers both MDM-linked and standalone assets.
type PickableAsset struct {
	AssetID      string
	AssetNumber  string
	Name         string
	Spec         string
	DeviceUdid   *string
	SerialNumber string
	Model        string
	OSVersion    string
	AssetStatus  string
	CategoryID   *string
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
	// MultiDayReason is the required justification when the rental spans more
	// than one calendar day AND at least one involved asset's category is
	// flagged daily_tracking_required (see CategoryRentalRule) — e.g. a
	// multi-day vehicle trip. Empty for ordinary single-day rentals.
	MultiDayReason string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// Joined fields (read-only)
	DeviceName   string
	DeviceSerial string
	AssetNumber  string
	AssetName    string
	CategoryID   *string
}

// CategoryRentalRule is a per-category opt-in for "daily tracking": a rental
// spanning multiple days needs a reason (Rental.MultiDayReason), and the
// borrower is expected to file one RentalDailyReport per day rather than a
// single report at final return. Resolved with the same "walk up
// categories.parent_id, first explicit row wins" inheritance that
// checklist_templates/category_notices reuse in a later phase — a category
// with no row here (at any ancestor) is simply not subject to the rule.
type CategoryRentalRule struct {
	CategoryID            string
	DailyTrackingRequired bool
	UpdatedBy             *string
	UpdatedAt             time.Time
}

// RentalDailyReport is one day's checklist for a daily_tracking_required
// rental (e.g. a day's odometer reading). Keyed by RentalNumber rather than a
// single Rental row's ID because rental actions operate on the whole batch
// sharing a rental_number, matching RentalRepo's *ByNumber methods.
// BackfillReason is non-empty when this was filed after the fact (the
// borrower forgot to report on ReportDate itself) rather than in real time.
type RentalDailyReport struct {
	ID             string
	RentalNumber   int
	ReportDate     time.Time
	Checklist      map[string]interface{}
	BackfillReason string
	ReportedBy     *string
	ReportedAt     time.Time
}

// --- Maintenance (Equipment dispatch) Management ---

// MaintenanceRequest is one asset line of a 資通設備進出及維護申請單.
// Multiple rows can share the same RequestNumber, mirroring Rental's
// batch-by-number pattern.
type MaintenanceRequest struct {
	ID             string
	RequestNumber  int
	AssetID        string
	ApplicantID    string
	ApplicantName  string
	Reason         string // 申請原因
	Vendor         string // 維修廠商
	Technician     string // 維修人員
	CheckoutDate   *time.Time
	ReturnDate     *time.Time
	ProcessNotes   string // 作業過程
	Status         string // pending, handler_signed, approved, returned, rejected
	HandlerID      *string
	HandlerName    string
	HandledAt      *time.Time
	SupervisorID   *string
	SupervisorName string
	ApprovedAt     *time.Time
	RejectReason   string
	IsArchived     bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// ISO 27001 fields — data-leakage risk on the outgoing device (A.7.9 /
	// A.8.10) and third-party loaner-device risk (A.8.1 / A.5.20).
	ContainsSensitiveData   bool   // 是否含機敏/個資資料
	VendorNDARef            string // 廠商保密協議編號/註記
	DataWipedBeforeCheckout bool   // 送修前已備份/已清除資料
	LoanerInfo              string // 替代機資訊（廠牌型號/序號）
	LoanerProvidedDate      *time.Time
	LoanerSecurityChecked   bool // 替代機已完成資安檢查
	LoanerReturnedDate      *time.Time
	// Joined fields (read-only)
	AssetName   string
	AssetNumber string
}

// MaintenanceApproveParams carries the fields captured at the moment the
// supervisor approves a maintenance request — the point where the asset
// physically leaves and, if the vendor supplies one, a loaner arrives.
type MaintenanceApproveParams struct {
	SupervisorID            string
	SupervisorName          string
	CheckoutDate            *time.Time
	DataWipedBeforeCheckout bool
	LoanerInfo              string
	LoanerProvidedDate      *time.Time
	LoanerSecurityChecked   bool
}

// MaintenanceReturnParams carries the fields captured when the asset (and
// any loaner) is checked back in.
type MaintenanceReturnParams struct {
	ReturnDate         *time.Time
	ProcessNotes       string
	LoanerReturnedDate *time.Time
}

// --- Disposal Management ---

// DisposalRequest is a 資訊資產報廢申請 application header, covering one or
// more assets (DisposalRequestItem rows).
type DisposalRequest struct {
	ID            string
	RequestNumber int
	ApplicantID   string
	ApplicantName string
	Status        string // pending, approved, rejected
	ApproverID    *string
	ApproverName  string
	ApprovedAt    *time.Time
	RejectReason  string
	IsArchived    bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// Joined
	Items []DisposalRequestItem
}

// DisposalRequestItem is one asset line within a DisposalRequest.
type DisposalRequestItem struct {
	ID              string
	DisposalID      string
	LineNo          int
	AssetID         string
	AssetName       string // snapshot at time of application
	AssetNumber     string // snapshot at time of application
	DisposeDate     *time.Time
	DisposeReason   string
	DataWipeChecked bool
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
	// SupportedPlatforms is a comma-separated list of lower-case platform
	// tokens (ios,ipados,macos,tvos,watchos). Used to filter App Store
	// search results and to gate "install" actions to compatible devices.
	SupportedPlatforms string
	CreatedAt          time.Time
	UpdatedAt          time.Time
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
	UDID              string
	SerialNumber      string
	DeviceName        string
	AssetNumber       string
	Model             string
	OSVersion         string
	LastSeen          time.Time
	EnrollmentStatus  string
	IsSupervised      bool
	IsLostMode        bool
	BatteryLevel      float64
	CustodianName     string
	CurrentHolderName string
	CategoryName      string
	CategoryID        *string
	CustodianID       *string
	AssetStatus       string
}

// MailSettings is the single-row configuration for outgoing + incoming mail.
type SSOSettings struct {
	Enabled      bool
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	UpdatedAt    time.Time
	UpdatedBy    string
}

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
	// SMTPCACert, when set, is an extra CA certificate (PEM) trusted for the
	// STARTTLS handshake — for mail servers using an internal/private CA.
	SMTPCACert string
	// SMTPInsecureSkipVerify disables TLS certificate verification entirely.
	// Explicit admin opt-in, off by default; prefer SMTPCACert instead.
	SMTPInsecureSkipVerify bool

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
