package controller

// -- Generic responses --

type swagOK struct {
	OK bool `json:"ok" example:"true"`
}

type swagError struct {
	Error string `json:"error" example:"error message"`
}

type swagIDResponse struct {
	ID string `json:"id" example:"uuid"`
}

// -- System --

type swagSystemStatus struct {
	Initialized bool `json:"initialized" example:"true"`
}

type swagSetupReq struct {
	Username    string `json:"username" example:"admin"`
	Password    string `json:"password" example:"secret123"`
	DisplayName string `json:"display_name" example:"Admin"`
}

type swagRegisterReq struct {
	Username    string `json:"username" example:"newuser"`
	Password    string `json:"password" example:"secret123"`
	DisplayName string `json:"display_name" example:"New User"`
}

type swagWSConfig struct {
	BackendRelay bool `json:"backend_relay" example:"true"`
}

// -- Auth --

type swagLoginReq struct {
	Username string `json:"username" example:"admin"`
	Password string `json:"password" example:"secret123"`
}

type swagLoginResp struct {
	ExpiresAt         string            `json:"expires_at"`
	User              swagLoginUser     `json:"user"`
	ModulePermissions map[string]string `json:"module_permissions"`
}

type swagLoginUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	SystemRole  string `json:"system_role"`
	DisplayName string `json:"display_name"`
}

type swagMeResp struct {
	ID                string            `json:"id"`
	Username          string            `json:"username"`
	Role              string            `json:"role"`
	SystemRole        string            `json:"system_role"`
	ModulePermissions map[string]string `json:"module_permissions"`
}

// -- Device --

type swagDevice struct {
	UDID             string  `json:"udid"`
	SerialNumber     string  `json:"serial_number"`
	DeviceName       string  `json:"device_name"`
	Model            string  `json:"model"`
	OSVersion        string  `json:"os_version"`
	LastSeen         string  `json:"last_seen"`
	EnrollmentStatus string  `json:"enrollment_status"`
	IsSupervised     bool    `json:"is_supervised"`
	IsLostMode       bool    `json:"is_lost_mode"`
	BatteryLevel     float64 `json:"battery_level"`
}

type swagDeviceListItem struct {
	UDID             string  `json:"udid"`
	SerialNumber     string  `json:"serial_number"`
	DeviceName       string  `json:"device_name"`
	Model            string  `json:"model"`
	OSVersion        string  `json:"os_version"`
	LastSeen         string  `json:"last_seen"`
	EnrollmentStatus string  `json:"enrollment_status"`
	IsSupervised     bool    `json:"is_supervised"`
	IsLostMode       bool    `json:"is_lost_mode"`
	BatteryLevel     float64 `json:"battery_level"`
	CustodianName    string  `json:"custodian_name"`
	CategoryName     string  `json:"category_name"`
	CategoryID       *string `json:"category_id"`
	CustodianID      *string `json:"custodian_id"`
	AssetStatus      string  `json:"asset_status"`
}

type swagDeviceListResp struct {
	Devices []swagDeviceListItem `json:"devices"`
	Total   int                  `json:"total"`
}

type swagDeviceAvailResp struct {
	Devices []swagDeviceAvailItem `json:"devices"`
}

type swagDeviceAvailItem struct {
	UDID             string `json:"udid"`
	SerialNumber     string `json:"serial_number"`
	DeviceName       string `json:"device_name"`
	Model            string `json:"model"`
	OSVersion        string `json:"os_version"`
	EnrollmentStatus string `json:"enrollment_status"`
	AssetStatus      string `json:"asset_status"`
}

type swagSyncCountResp struct {
	Count int `json:"count"`
}

// -- Asset --

type swagAssetReq struct {
	DeviceUdid    *string `json:"device_udid"`
	AssetNumber   string  `json:"asset_number"`
	Name          string  `json:"name"`
	Spec          string  `json:"spec"`
	Quantity      int     `json:"quantity"`
	Unit          string  `json:"unit"`
	AcquiredDate  *string `json:"acquired_date" example:"2025-01-01"`
	UnitPrice     float64 `json:"unit_price"`
	Purpose       string  `json:"purpose"`
	BorrowDate    *string `json:"borrow_date" example:"2025-06-01"`
	CustodianID   *string `json:"custodian_id"`
	CustodianName string  `json:"custodian_name"`
	Location      string  `json:"location"`
	AssetCategory string  `json:"asset_category"`
	Notes         string  `json:"notes"`
	CategoryID    *string `json:"category_id"`
}

type swagDeviceStatusReq struct {
	UDID   string `json:"udid" example:"00008101-001234560A12001E"`
	Status string `json:"status" example:"available" enums:"available,faulty,repairing,retired"`
}

// swagAssetBatchUpdateFields documents the only fields the batch-update
// endpoint accepts — category/location/purpose/is_rentable. Custodian and
// dispose/transfer state go through their own audited endpoints instead.
type swagAssetBatchUpdateFields struct {
	CategoryID *string `json:"category_id,omitempty"`
	Location   *string `json:"location,omitempty"`
	Purpose    *string `json:"purpose,omitempty"`
	IsRentable *bool   `json:"is_rentable,omitempty"`
}

type swagAssetBatchUpdateReq struct {
	IDs    []string                   `json:"ids"`
	Fields swagAssetBatchUpdateFields `json:"fields"`
}

// -- Rental --

type swagRentalReq struct {
	DeviceUdids []string `json:"device_udids"`
	BorrowerID  string   `json:"borrower_id"`
	Purpose     string   `json:"purpose"`
	BorrowDate  *string  `json:"borrow_date" example:"2025-12-01"`
	// MultiDayReason is required when the booking spans more than one day AND
	// touches a category flagged daily_tracking_required (e.g. a vehicle).
	MultiDayReason string  `json:"multi_day_reason"`
	ExpectedReturn *string `json:"expected_return" example:"2025-12-31"`
	Notes          string  `json:"notes"`
}

// swagDailyReportReq is the body for POST /api/rentals/{id}/daily-report.
type swagDailyReportReq struct {
	Checklist map[string]interface{} `json:"checklist"`
	// ReportDate backfills a past day (must be within the rental's date
	// range and not already reported); omit it to report for today.
	ReportDate *string `json:"report_date" example:"2025-12-02"`
	// BackfillReason is required whenever ReportDate is set.
	BackfillReason string `json:"backfill_reason"`
}

type swagRentalCreateResp struct {
	IDs          []string `json:"ids"`
	Count        int      `json:"count"`
	RentalNumber int      `json:"rental_number"`
}

type swagArchiveReq struct {
	IDs []string `json:"ids"`
}

type swagReturnReq struct {
	Notes     string                 `json:"notes"`
	Checklist map[string]interface{} `json:"checklist"`
}

// -- Maintenance --

type swagMaintenanceReq struct {
	AssetIDs              []string `json:"asset_ids"`
	ApplicantID           string   `json:"applicant_id"`
	Reason                string   `json:"reason"`
	Vendor                string   `json:"vendor"`
	Technician            string   `json:"technician"`
	CheckoutDate          *string  `json:"checkout_date" example:"2025-06-01"`
	ReturnDate            *string  `json:"return_date" example:"2025-06-10"`
	ContainsSensitiveData bool     `json:"contains_sensitive_data"`
	VendorNDARef          string   `json:"vendor_nda_ref"`
}

// swagMaintenanceApproveReq is the body for POST /api/maintenance-requests/{id}/approve.
// Captures the ISO 27001 checks performed at the moment the device physically
// leaves and, if the vendor supplies one, a loaner arrives.
type swagMaintenanceApproveReq struct {
	CheckoutDate            *string `json:"checkout_date" example:"2025-06-01"`
	DataWipedBeforeCheckout bool    `json:"data_wiped_before_checkout"`
	LoanerInfo              string  `json:"loaner_info" example:"廠牌型號/序號"`
	LoanerProvidedDate      *string `json:"loaner_provided_date" example:"2025-06-01"`
	LoanerSecurityChecked   bool    `json:"loaner_security_checked"`
}

// swagMaintenanceReturnReq is the body for POST /api/maintenance-requests/{id}/return.
type swagMaintenanceReturnReq struct {
	ReturnDate         *string `json:"return_date" example:"2025-06-10"`
	ProcessNotes       string  `json:"process_notes"`
	LoanerReturnedDate *string `json:"loaner_returned_date" example:"2025-06-10"`
}

type swagMaintenanceCreateResp struct {
	IDs           []string `json:"ids"`
	Count         int      `json:"count"`
	RequestNumber int      `json:"request_number"`
}

// -- Disposal --

type swagDisposalItemReq struct {
	AssetID         string  `json:"asset_id"`
	DisposeDate     *string `json:"dispose_date" example:"2025-06-01"`
	DisposeReason   string  `json:"dispose_reason"`
	DataWipeChecked bool    `json:"data_wipe_checked"`
}

type swagDisposalReq struct {
	ApplicantID string                `json:"applicant_id"`
	Items       []swagDisposalItemReq `json:"items"`
}

type swagDisposalCreateResp struct {
	ID            string `json:"id"`
	RequestNumber int    `json:"request_number"`
}

// -- App --

type swagManagedAppReq struct {
	Name          string `json:"name"`
	BundleID      string `json:"bundle_id"`
	AppType       string `json:"app_type" example:"vpp" enums:"vpp,enterprise"`
	ItunesStoreID string `json:"itunes_store_id"`
	ManifestURL   string `json:"manifest_url"`
	PurchasedQty  int    `json:"purchased_qty"`
	Notes         string `json:"notes"`
	IconURL       string `json:"icon_url"`
}

type swagAppActionReq struct {
	AppID string `json:"app_id"`
	UDID  string `json:"udid"`
}

type swagCommandResp struct {
	OK          bool        `json:"ok"`
	CommandUUID string      `json:"command_uuid"`
	RawResponse interface{} `json:"raw_response"`
}

type swagSyncAppsResp struct {
	OK     bool `json:"ok"`
	Synced int  `json:"synced"`
}

// -- User --

type swagUserListItem struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	IsActive    bool   `json:"is_active"`
}

type swagUserUpdateReq struct {
	Role        string `json:"role,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	IsActive    *bool  `json:"is_active,omitempty"`
	Password    string `json:"password,omitempty"`
}

// -- Category --

type swagCategoryItem struct {
	ID        string  `json:"id"`
	ParentID  *string `json:"parent_id"`
	Name      string  `json:"name"`
	Level     int     `json:"level"`
	SortOrder int     `json:"sort_order"`
}

type swagCategoryReq struct {
	ParentID *string `json:"parent_id"`
	Name     string  `json:"name"`
}

type swagCategoryUpdateReq struct {
	Name string `json:"name"`
}

// -- Profile --

type swagProfileItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Filename   string `json:"filename"`
	Size       int    `json:"size"`
	UploadedBy string `json:"uploaded_by"`
	CreatedAt  string `json:"created_at"`
}

type swagProfileContentResp struct {
	ContentBase64 string `json:"content_base64"`
	Filename      string `json:"filename"`
}

// -- Notification --

type swagNotificationItem struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Event       string  `json:"event"`
	Recipient   string  `json:"recipient"`
	Subject     string  `json:"subject"`
	Status      string  `json:"status"`
	ReferenceID *string `json:"reference_id"`
	CreatedAt   string  `json:"created_at"`
}
