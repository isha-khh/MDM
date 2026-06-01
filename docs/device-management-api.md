# Device Management API Documentation

> **Source:** https://developer.apple.com/documentation/DeviceManagement  
> **Type:** Web Service  
> **Description:** Manage your organization's devices remotely.

## Availability

- iOS 13.0+
- iPadOS 13.0+
- Mac Catalyst 13.0+
- macOS 10.15+
- tvOS 13.0+
- visionOS 1.1+
- watchOS 6.0+
- Device Assignment Services 5.0+
- VPP License Management 1.0+

---

## Overview

Deploying a mobile device management (MDM) solution allows administrators to securely and remotely configure enrolled devices. Administrators use Apple School Manager or Apple Business to enroll organization-owned devices, and users can enroll their own devices. After a device is enrolled, administrators can update software and device settings, monitor compliance with organizational policies, remotely erase or lock devices, and install apps and books developed in-house or purchased through Apple School Manager or Apple Business.

MDM works with Managed App Distribution to provide a seamless download and launch experience. For more information, see [ManagedAppDistribution](https://developer.apple.com/documentation/ManagedAppDistribution).

---

## Topics

### Configuration Profiles

#### [Configuring Multiple Devices Using Profiles](https://developer.apple.com/documentation/devicemanagement/configuring-multiple-devices-using-profiles)
Create and deploy configuration profiles to users within your organization.

#### [Profile-Specific Payload Keys](https://developer.apple.com/documentation/devicemanagement/profile-specific-payload-keys)
Use the appropriate payload for your configuration needs.

**In addition to standard payload keys, each payload contains type-specific keys. Topics include:**

- **Top Level:** `TopLevel`, `CommonPayloadKeys`
- **Accounts:** `Accounts`, `CalDAV`, `CardDAV`, `GoogleAccount`, `LDAP`, `MobileAccounts`, `SubscribedCalendars`
- **AirPlay:** `AirPlay`, `AirPlaySecurity`
- **App Management:** `AppLock`, `AssociatedDomains`, `AutonomousSingleAppMode`, `NSExtensionManagement`
- **App Store:** `AppStore`
- **Apple TV:** `ConferenceRoomDisplay`, `TVRemote`
- **Authentication:** `DirectoryService`, `ExtensibleSingleSignOn`, `ExtensibleSingleSignOnKerberos`, `Identification`, `IdentityPreference`, `SingleSignOn`
- **Certificates:** `ACMECertificate`, `ActiveDirectoryCertificate`, `CertificatePEM`, `CertificatePKCS1`, `CertificatePKCS12`, `CertificateRoot`, `CertificatePreference`, `CertificateRevocation`, `CertificateTransparency`, `SCEP`
- **Ethernet:** `8021XGlobalEthernet`, `8021XFirstActiveEthernet`, `8021XFirstEthernet`, etc.
- **Full Disk Encryption:** `FDEFileVault`, `FDEFileVaultOptions`, `FDERecoveryKeyEscrow`
- **Login:** `LoginItemsManagedItems`, `LoginWindowLoginItems`, `LoginWindow`, `LoginWindowScripts`, `ServiceManagementManagedLoginItems`
- **Mail:** `ExchangeActiveSync`, `ExchangeWebServices`, `Mail`
- **Managed Devices:** `EducationConfiguration`, `LightsOutManagementLOM`, `ManagedPreferences`, `MDM`, `ProfileRemovalPassword`
- **Media Management:** `MediaManagementDiscBurning`
- **Networking:** `Cellular`, `CellularPrivateNetwork`, `ContentCaching`, `DNSSettings`, `Domains`, `Firewall`, `NetworkUsageRules`, `Relay`, `WiFi`, `WiFiManagedSettings`
- **Parental Controls:** `ParentalControlsApplicationRestrictions`, `ParentalControlsContentFilter`, `ParentalControlsDictionary`, `ParentalControlsGameCenter`, `ParentalControlsTimeLimits`
- **Preferences:** `GlobalPreferences`, `UserPreferences`
- **Printing:** `AirPrint`, `Printing`
- **Privacy:** `PrivacyPreferencesPolicyControl`
- **Proxies:** `DNSProxy`, `GlobalHTTPProxy`, `NetworkProxyConfiguration`
- **Restrictions:** `Restrictions`
- **Security:** `Passcode`, `SecurityPreferences`, `SmartCard`
- **System Configuration:** `Declarations`, `EnergySaver`, `FileProvider`, `Font`, `LockScreenMessage`, `Screensaver`, `SystemExtensions`, `SystemLogging`, `TimeServer`
- **System Policy:** `SystemPolicyControl`, `SystemPolicyKernelExtensions`, `SystemPolicyManaged`, `SystemPolicyRule`
- **System Updates:** `SoftwareUpdate`, `SystemMigration`
- **User Experience:** `Accessibility`, `Desktop`, `Dock`, `Finder`, `HomeScreenLayout`, `ManagedMenuExtras`, `Notifications`, `ScreensaverUser`, `SetupAssistant`, `TimeMachine`
- **VPN:** `AppLayerVPN`, `AppToAppLayerVPNMapping`, `VPN`
- **Web:** `WebClip`, `WebContentFilter`
- **Xsan:** `Xsan`, `XsanPreferences`
- **Deprecated:** `AIMAccount`, `APN`, `FDERecoveryKeyRedirection`, `JabberAccount`, `MacOSServerAccount`, `MediaManagementAllowedMedia`, `ParentalControlsDashboardWidgetRestrictions`, `ParentalControlDictationAndProfanity`, `ShareKit`, `SystemPreferences`

---

### MDM Protocol

#### [Implementing Device Management](https://developer.apple.com/documentation/devicemanagement/implementing-device-management)
Set up an MDM server and send commands to managed devices.

**Topics:**

- **Essentials**
  - Managing MDM Connections — Establish or remove a connection between a device and an MDM server.
  - Simplifying MDM Server Administration for iOS Devices — Create a service configuration entry point to your MDM server to access frequently used information.
- **Certificates and Profiles**
  - Managing Certificates for MDM Servers and Devices — Ensure secure MDM connectivity with valid certificates.
  - Deploying MDM Enrollment Profiles — Choose the technique to deploy MDM enrollment profiles for your organization.
  - Installing Profiles on Devices — Optimize deployment of profiles and provisioning profiles.
  - Setting Up Push Notifications for Your MDM Customers — Create and sign a certificate signing request (CSR) to enable push notifications.
- **Identity Management**
  - Validating a Managed Device Attestation — Verify an attestation that a managed device returns by performing the required steps.
- **Devices and Users**
  - Managing MDM Devices and Users in macOS — Manage devices and users as separate entities in macOS.
  - Enabling Network and Mobile User Logins — Manage network users on macOS devices bound to an Open Directory server, and mobile users on shared iPads.
  - Managing Passcodes — Ensure data security by managing device passcodes and compliance with policies.
  - Dealing with Inactive MDM Devices and Invalid Push Tokens — Handle when devices become unmanageable due to inactivity or invalid push tokens.
  - Returning a managed device to service — Use a device management service to return managed devices to service quickly after use.
- **Commands**
  - Sending MDM Commands to a Device — Execute commands on a device and receive responses that contain the results of each operation.
  - Handling NotNow Status Responses — Handle when a device won't execute a command and instead returns a NotNow status.

---

#### [Commands and Queries](https://developer.apple.com/documentation/devicemanagement/commands-and-queries)
Manage the configuration and behavior of your devices.

> **Important:** Mobile Device Management is for enterprise use only. To use it in your app, the Account Holder of your app's development team must request the Mobile Device Management capability.

**Topics:**

- **Profile Management**
  - Install Profile — Install a configuration profile on a device.
  - Profile List — Get a list of installed profiles on a device.
  - Remove Profile — Remove a previously installed profile from the device.
  - Install Provisioning Profile — Install a provisioning profile on a device.
  - Provisioning Profile List — Get a list of installed provisioning profiles on a device.
  - Remove Provisioning Profile — Remove a previously installed provisioning profile from a device.
- **Device Details**
  - Device Information — Get detailed information about a device.
  - Device Configured — Inform the device that it can allow the user to continue in Setup Assistant.
  - User Configured — Inform the device that it can continue past Setup Assistant and finish login.
  - Restrictions — Get a list of restrictions on the device.
- **Device State**
  - Erase Device — Remotely and immediately erase a device.
  - Device Lock — Remotely and immediately lock a device.
  - Restart Device — Remotely and immediately restart a device.
  - Shut Down Device — Remotely and immediately shut down a device.
- **Managed Apps**
  - Install Application — Install a third-party app on a device.
  - Install Enterprise Application — Install an enterprise app on a device.
  - Installed Application List — Get a list of the installed apps on a device.
  - Managed Application List — Get the status of all managed apps on a device.
  - Remove Application — Remove an app.
  - Apply Redemption Code — Complete the installation of an app using a redemption code.
  - Validate Applications — Force validation of developer and universal provisioning profiles for enterprise apps.
  - Managed Application Attributes — Query attributes in managed apps on a device.
  - Managed Application Configuration — Get app configurations from managed apps on a device.
  - Managed Application Feedback — Get app feedback from a managed app on the device.
- **Managed Media**
  - Install Media — Install a book on a device.
  - Managed Media List — Get a list of the managed books on a device.
  - Remove Media — Remove a previously installed book from a device.
- **Accounts**
  - Account Configuration — Create and configure a local administrator account on a device.
  - Invite To Program — Invite a user to join the Volume Purchase Program (VPP).
- **Passwords**
  - Clear Passcode — Remove the passcode from a device.
  - Clear Restrictions Password — Clear the Screen Time password and the restrictions on a device.
  - Unlock User Account — Unlock a user account that the system locked because of too many failed password attempts.
  - Set Auto Admin Password — Update the local administrator account password.
  - Set Firmware Password — Change or clear the firmware password on a device.
  - Verify Firmware Password — Verify the firmware password on a device.
- **Updates** (Deprecated)
  - Schedule OS Update Scan — Schedule a background scan for operating-system updates on a device.
  - Available OS Updates — Get a list of available operating-system updates for a device. *(Deprecated)*
  - Schedule OS Update — Schedule an update of the operating system on a device. *(Deprecated)*
  - OS Update Status — Get the status of operating-system updates on a device. *(Deprecated)*
- **Lost Device**
  - Enable Lost Mode — Enable Lost Mode on a device, which provides a message and phone number on the Lock Screen.
  - Device Location — Request the location of a device when in Lost Mode.
  - Play Lost Mode Sound — Play the Lost Mode sound on a device that's in Lost Mode.
  - Disable Lost Mode — Take the device out of Lost Mode.
- **Recovery Lock**
  - Set Recovery Lock — Set or clear the Recovery Lock password.
  - Verify Recovery Lock — Verify the device's Recovery Lock password.
- **Content Caching**
  - Content Caching Information — Get the status of the content caches on a device.
- **AirPlay Mirroring**
  - Request Mirroring — Prompt the user to share their screen using AirPlay Mirroring.
  - Stop Mirroring — Stop mirroring the display to another device.
- **eSIM Management**
  - Refresh Cellular Plans — Query a carrier URL for active eSIM cellular-plan profiles on a device.
- **Managed Settings**
  - Disable Remote Desktop — Disable Remote Desktop on a device.
  - Enable Remote Desktop — Enable Remote Desktop on a device.
  - Settings — Configure settings on a device.
- **Lights-Out Management**
  - LOM Device Request — Send requests to a device using lights-out management (LOM).
  - LOM Setup Request — Get information from a device to set up lights-out management (LOM).
- **Security**
  - Security Info — Get security-related information about a device.
  - Certificate List — Get a list of installed certificates on a device.
  - Activation Lock Bypass Code — Get the code to bypass Activation Lock on a device.
  - Clear Activation Lock Bypass Code — Clear the Activation Lock bypass code on a device.
  - Rotate FileVault Key — Change the FileVault primary password on a device.
- **Extensions**
  - Active NSExtensions — Get a list of active extensions for a user on a device.
  - NSExtension Mappings — Get a list of the installed extensions for a user on a device.
- **User Management**
  - User List — Get a list of users with active accounts on a device.
  - Log Out User — Force the current user to log out of a device.
  - Delete User — Delete a user's account from a device.
- **Declarative Management**
  - Declarative Management — Enable your server to support declarative management or trigger a declarative management synchronization operation on the device.

---

#### [Check-in](https://developer.apple.com/documentation/devicemanagement/check-in)
Authenticate devices and maintain push tokens with these commands.

The MDM check-in protocol validates a device's eligibility for MDM enrollment and informs the server that a device's push token has been updated. When the MDM payload is installed, the device initiates communication with the check-in server.

**Topics:**

- **Commands**
  - Authenticate — Authenticates a user during MDM payload installation.
  - User Authenticate — Authenticates a user with a two-step authentication protocol.
  - Check Out — Responds to the removal of the MDM enrollment profile from a device.
  - Get Token — Gets a token from the server.
  - Token Update — Updates the token for a device on the server.
  - Get Bootstrap Token — Gets the bootstrap token from the server.
  - Set Bootstrap Token — Sends the bootstrap token to the server.
  - Return To Service — Gets the return-to-service configuration from the server.
- **Declarative Management**
  - Declarative Management — Sends declarative management requests to the server.
  - Get Server Supported Declarations — Get a list of the declarations available on the server.
  - Get the Device Status — The request for getting the status of a device.
  - Get the Device Token — The request for sending the device token details.

---

#### [Account-driven enrollment](https://developer.apple.com/documentation/devicemanagement/account-driven-enrollment)
Authenticate devices using a user identity-focused workflow.

**Topics:**

- **Sign-in methods**
  - Onboarding users with account sign-in — Implement user-initiated, identity-focused authentication flows.
  - Enrolling with Platform Single Sign-on — Authenticate users during device enrollment using Platform Single Sign-on.
- **Objects**
  - `EnrollmentSSODocument` — Enrollment SSO streamlines the MDM enrollment process, reduces sign-ins, and improves security.
  - Discover Authentication Servers — Get a list of available authentication servers.

---

#### [Migrating managed devices](https://developer.apple.com/documentation/devicemanagement/migrating-managed-devices)
Migrate managed devices from one device management service to another.

---

### Declarative Management

#### [Leveraging the declarative management data model to scale devices](https://developer.apple.com/documentation/devicemanagement/leveraging-the-declarative-management-data-model-to-scale-devices)
Use declarative management to make devices more autonomous and proactive.

#### [Integrating Declarative Management](https://developer.apple.com/documentation/devicemanagement/integrating-declarative-management)
Use the declarative management protocol to manage MDM features such as device enrollment and un-enrollment and device and user authentication.

#### [Deploying apps with declarative management](https://developer.apple.com/documentation/devicemanagement/deploying-apps-with-declarative-management)
Use declarative app configurations to deploy managed apps to devices.

#### [Declarations](https://developer.apple.com/documentation/devicemanagement/devicemanagement-declarations)
The available declarations for device management.

**Topics:**

- **Configurations**
  - `AccountCalDAV` — Configure a Calendar account.
  - `AccountCardDAV` — Configure a Contacts account.
  - `AccountExchange` — Configure an Exchange account.
  - `AccountGoogle` — Configure a Google account.
  - `AccountLDAP` — Configure an LDAP account.
  - `AccountMail` — Configure a Mail account.
  - `AccountSubscribedCalendar` — Configure a subscribed calendar.
  - `AppManaged` — Configure a managed app.
  - `AudioAccessorySettings` — Configure audio accessory settings.
  - `DiskManagementSettings` — Configure disk management settings on the device.
  - `ExternalIntelligenceSettings` — Configure External Intelligence Integrations settings.
  - `IntelligenceSettings` — Configure Apple Intelligence settings.
  - `KeyboardSettings` — Configure keyboard settings.
  - `LegacyInteractiveProfile` — Configure an interactive legacy profile.
  - `LegacyProfile` — Configure a legacy profile.
  - `ManagementStatusSubscriptions` — Configure status subscriptions.
  - `ManagementTest` — Test declarative device management.
  - `MigrationAssistantSettings` — Configure Migration Assistant settings.
  - `MathSettings` — Configure the math and calculator apps.
  - `Package` — Install a package.
  - `PasscodeSettings` — Configure passcode policy settings.
  - `SafariBookmarks` — Configure managed bookmarks in Safari.
  - `SafariExtensionSettings` — Configure Safari Extensions.
  - `SafariSettings` — Configure Safari settings.
  - `ScreenSharingConnection` — Configure a connection to a screen-sharing host.
  - `ScreenSharingConnectionGroup` — Configure a group of screen-sharing connections.
  - `ScreenSharingHostSettings` — Configure screen-sharing host settings and restrictions.
  - `SecurityCertificate` — Add a certificate to the device.
  - `SecurityIdentity` — Install an identity on the device.
  - `SecurityPasskeyAttestation` — Configure the device to allow WebAuthn enterprise attestation for certain passkeys.
  - `ServicesBackgroundTasks` — Configure background tasks.
  - `ServicesConfigurationFiles` — Managed configuration files for services.
  - `SiriSettings` — Configure Siri settings.
  - `SoftwareUpdateEnforcementSpecific` — A software update enforcement policy for a specific OS release.
  - `SoftwareUpdateSettings` — Configure software updates.
  - `WatchEnrollment` — Configure an MDMv1 profile for Apple Watch enrollment.
- **Activations**
  - `ActivationSimple` — Activate a set of configurations.
- **Assets**
  - `AssetCredentialACME` — A reference to an ACME identity.
  - `AssetCredentialCertificate` — A reference to a PKCS #1 or PEM encoded certificate.
  - `AssetCredentialIdentity` — A reference to a PKCS #12 password-protected identity.
  - `AssetCredentialSCEP` — A reference to a SCEP identity.
  - `AssetCredentialUserNameAndPassword` — A reference to a credential representing a user name and password.
  - `AssetData` — A reference to arbitrary data with a specific media type.
  - `AssetUserIdentity` — The user-identity data.
- **Credentials**
  - `ACMECredential` — An ACME identity that the device generates.
  - `IdentityCredential` — Data for a PKCS #12 password-protected identity.
  - `SCEPCredential` — A SCEP identity that the device generates.
  - `UserNameAndPasswordCredential` — Data describing a credential that represents a user name and password.
- **Management**
  - `ManagementOrganizationInformation` — Configure the managing organization's contact information.
  - `ManagementProperties` — Configure the properties on the device.
  - `ManagementServerCapabilities` — Configure the server's feature set.
- **Base Declaration**
  - `DeclarationBase` — Keys common to all declarations used with the Remote Management protocol.

---

#### [Status Reports](https://developer.apple.com/documentation/devicemanagement/status-reports)
Reports from the device about its current state.

**Topics:**

- **Status Reports and Elements**
  - `StatusReport`
  - `StatusAppManagedList` — The device's declarative managed apps.
  - `StatusDeviceBatteryHealth` — The device's battery health.
  - `StatusDeviceModelFamily` — A status report of the device's hardware family.
  - `StatusDeviceModelIdentifier` — A status report of the device's hardware identifier.
  - `StatusDeviceModelMarketingName` — A status report of the device's marketing name.
  - `StatusDeviceModelNumber` — A status report of the device's hardware number.
  - `StatusDeviceOperatingSystemBuildVersion` — A status report of the device's software build identifier.
  - `StatusDeviceOperatingSystemFamily` — A status report of the device's operating system family.
  - `StatusDeviceOperatingSystemMarketingName` — A status report of the device's OS marketing name.
  - `StatusDeviceOperatingSystemSupplementalBuildVersion` — OS supplemental build identifier.
  - `StatusDeviceOperatingSystemSupplementalExtraVersion` — OS Background Security Improvement identifier.
  - `StatusDeviceOperatingSystemVersion` — A status report of the device's OS version.
  - `StatusDeviceSerialNumber` — A status report of the device's serial number.
  - `StatusDeviceUDID` — A status report of the device's UDID.
  - `StatusDiskManagementFileVaultEnabled` — The enabled status of the File Vault.
  - `StatusManagementClientCapabilities` — A status report of the client's protocol capabilities.
  - `StatusManagementDeclarations` — A status report of the client's processed declarations.
  - `StatusMigrationAssistantReport` — Reports the status of a completed migration.
  - `StatusMigrationAssistantState` — The current migration state of the system.
  - `StatusMDMApp` — A status report of the client's MDM-installed apps.
  - `StatusPackageList` — The client's declarative packages.
  - `StatusPasscodeCompliance` — A status report of passcode compliance.
  - `StatusPasscodeIsPresent` — A status report of the passcode on the device.
  - `StatusReason` — Provides details about an error for an item in a status report.
  - `StatusScreenSharingConnectionGroupUnresolvedConnections` — Information about connection groups with unresolvable member references.
  - `StatusSecurityCertificateList` — A status report of the client's managed certificates.
  - `StatusServicesBackgroundTask` — A status report of the device's background task details.
  - `StatusSoftwareUpdateBetaEnrollment` — A status report of the device's enrolled beta program.
  - `StatusSoftwareUpdateDeviceID` — A status report of the device's update device ID.
  - `StatusSoftwareUpdateFailureReason` — A status report of a software update failure reason.
  - `StatusSoftwareUpdateInstallReason` — A status report of the reason for a pending software update.
  - `StatusSoftwareUpdateInstallState` — A status report of the software update install state.
  - `StatusSoftwareUpdatePendingVersion` — A status report of the pending software update version.
- **Status Account List Elements**
  - `StatusAccountListCalDAV` — Calendar accounts.
  - `StatusAccountListCardDAV` — Contacts accounts.
  - `StatusAccountListExchange` — Exchange accounts.
  - `StatusAccountListGoogle` — Google accounts.
  - `StatusAccountListLDAP` — LDAP accounts.
  - `StatusAccountListMailIncoming` — Incoming Mail accounts.
  - `StatusAccountListMailOutgoing` — Outgoing Mail accounts.
  - `StatusAccountListSubscribedCalendar` — Subscribed calendars.
- **Status Test Elements**
  - `StatusTestArrayValue` — A test status item for an array.
  - `StatusTestBooleanValue` — A test status item for a Boolean value.
  - `StatusTestDictionaryValue` — A test status item for a dictionary.
  - `StatusTestErrorValue` — A test status item for an error.
  - `StatusTestIntegerValue` — A test status item for an integer.
  - `StatusTestRealValue` — A test status item for a real value.
  - `StatusTestStringValue` — A test status item for a string.

---

### Deployment Services

#### [Device Assignment](https://developer.apple.com/documentation/devicemanagement/device-assignment)
Manage devices for your students and employees.

**Topics:**

- **Authentication**
  - Authenticating with a Device Enrollment Program (DEP) Server — Communicate securely with a DEP web service, using a server token.
  - Authenticating Through Web Views — Use your own custom web interfaces to authenticate users.
- **Device Management**
  - Activation Lock a Device — Enable activation lock on a remote device.
  - Get Device Details — Get the details on a set of devices.
  - Get a List of Devices — Get a list of devices that are managed by the server.
  - Sync the List of Devices — Get updates about the list of devices the server manages.
  - Disown Devices — Notify Apple's servers that your organization no longer owns the specified devices.
  - Get Beta Enrollment Tokens — Retrieves the beta enrollment tokens available for the organization.
- **Profile Management**
  - Define a Profile — Define a profile that can be distributed to the devices in your organization.
  - Get a Profile — Get details about a profile.
  - Assign a Profile — Assign a profile to a list of devices.
  - Remove a Profile — Remove a profile from a list of devices.
- **Account-Driven Enrollment Service Discovery**
  - Assign Account-Driven Enrollment Service Discovery — The Account-Driven Enrollment profile defines key attributes related to service discovery.
  - Fetch Account-Driven Enrollment Service Discovery — Fetch the Account-Driven Enrollment profile that the MDM server sets.
  - Remove Account-Driven Enrollment Profile — Remove the Account-Driven Enrollment profile that the MDM server sets.
- **Objects and Data Types**
  - `Device` — A device's properties and their values.
  - `MachineInfo` — A device's information in response to a MDM enrollment profile request.
  - `Profile` — A profile's properties and their values.
  - `Limit` — A ranged limit.
  - `Url` — A URL object.

---

#### [Roster Management](https://developer.apple.com/documentation/devicemanagement/roster-management)
Manage classes for your students and teachers.

> Administrators of Apple School Manager can use this API to access information about classes and people in their organization. **Not supported for Apple Business organizations.**

**Topics:**

- **Account Management**
  - Get Account Detail — Obtain the details for your account.
- **Class Management**
  - `RosterClass` — A class's properties and their values.
  - Get the List of Classes — Obtain a list of classes the server manages.
  - Sync the List of Classes — Get updates about the list of classes the server manages.
- **People Management**
  - `RosterPerson` — A person's properties and their values.
  - Get the List of People — Obtain a list of people the server manages.
  - Sync the List of People — Get updates about the list of people the server manages.
- **Course Management**
  - `BaseRosterCourse` — A base course's properties and their values.
  - `RosterCourse` — A course's properties and their values.
  - Get the List of Courses — Obtain a list of the courses the server manages.
  - Sync the List of Courses — Get updates about the list of courses the server manages.
- **Location Management**
  - `BaseRosterLocation` — A base location's properties and their values.
  - `RosterLocation` — A location's properties and their values.
  - Get the List of Locations — Obtain a list of the locations the server manages.
  - Sync the Locations — Get updates about the list of locations the server manages.

---

#### [App and Book Management](https://developer.apple.com/documentation/devicemanagement/app-and-book-management)
Manage apps and books for your students and employees.

**Topics:**

- **Essentials**
  - Managing Apps and Books Through Web Services — Associate app and book purchases with users or devices.
  - Upgrading to the new App and Book Management API — Manage devices and content across your organization using the new API version.
  - Apps and Books for Organizations — Get details about apps and books to show to your users.
  - Managing Assets — Retrieve key information to effectively manage assets across an organization's users and devices.
  - Managing Users — Retrieve key information to effectively manage users across an organization.
  - Using Paginated Endpoints — Manage paginated endpoints to efficiently work with large record sets.
  - Subscribing to Notifications — Listen to notifications to keep track of the latest events for an organization.
  - Handling Error Responses — Investigate service request errors and troubleshoot solutions.
- **Configuration Management**
  - Client Config — Store client-specific information on the server.
  - Service Config — Provides the full list of web service URLs, notification types, request limits, and possible error codes.
- **Asset Management**
  - Get Assets — Get the set of assets that your organization manages.
  - Associate Assets — Associate assets with client user IDs and serial numbers.
  - Disassociate Assets — Disassociate assets from client user IDs and serial numbers.
  - Revoke Assets — Revoke assets from client user IDs and serial numbers.
  - Get Assignments — Get the set of current assignments for users or devices.
- **User Management**
  - Get Users — Get information about a set of users.
  - Create Users — Create users to assign apps and books to.
  - Update Users — Update details for existing users.
  - Retire Users — Retire users by client user IDs.
- **Event Management**
  - Event Status — Retrieve the status of an asynchronous event.
- **Objects and Data Types**
  - `Asset` — A product in the store.
  - `ResponseAsset` — The asset that the organization owns.
  - `Assignment` — The asset assignment for a user or device.
  - `RequestUser` — The requested user in the organization.
  - `ResponseUser` — The user in the organization.
- **Legacy API**
  - App and Book Management (Legacy) — Manage apps and books for your students and employees.

---

### Endpoints

| Endpoint | Description |
|----------|-------------|
| [Fetch a apps resource's relationship](https://developer.apple.com/documentation/devicemanagement/fetch-a-apps-resource's-relationship) | — |
| [Fetch a books resource's relationship](https://developer.apple.com/documentation/devicemanagement/fetch-a-books-resource's-relationship) | — |
| [Get Multiple Genres](https://developer.apple.com/documentation/devicemanagement/get-multiple-genres) | Fetch metadata for genres from the catalog by using their identifiers. |
| [Get a Genre](https://developer.apple.com/documentation/devicemanagement/get-a-genre) | Fetch metadata for a genre from the catalog by using its identifier. |

---

### Dictionaries

| Object | Description |
|--------|-------------|
| [`ManifestURL`](https://developer.apple.com/documentation/devicemanagement/manifesturl) | The URL to the app manifest. |
| [`PasswordHash`](https://developer.apple.com/documentation/devicemanagement/passwordhash) | A dictionary that contains the password hash for the account. |
| [`RelationshipResponse`](https://developer.apple.com/documentation/devicemanagement/relationshipresponse) | — |
| [`ResponseErrorCode`](https://developer.apple.com/documentation/devicemanagement/responseerrorcode) | An error code. |

---

*Documentation scraped from [Apple Developer Documentation](https://developer.apple.com/documentation/DeviceManagement) on 2026-06-01.*
