# Apple Business API Endpoints

> **Base URL:** `https://api-business.apple.com/`

> **API Version:** Apple Business API 2.0+

> **Source:** [Apple Developer Documentation](https://developer.apple.com/documentation/applebusinessapi)

---

## Table of Contents

- [Devices](#devices)
  - [Get Organization Devices](#get-organization-devices)
  - [Get Device Information](#get-device-information)
  - [Get AppleCare coverage Information for a Device](#get-applecare-coverage-information-for-a-device)
  - [Get Devices Enrolled in Apple Device Management Service](#get-devices-enrolled-in-apple-device-management-service)
  - [Get Details for a Device Enrolled in Apple Device Management Service](#get-details-for-a-device-enrolled-in-apple-device-management-service)
- [Device Management Services](#device-management-services)
  - [Get Device Management Services](#get-device-management-services)
  - [Get the Device Serial Numbers for a Device Management Service](#get-the-device-serial-numbers-for-a-device-management-service)
  - [Get the Assigned Device Management Service ID for a Device](#get-the-assigned-device-management-service-id-for-a-device)
  - [Get the Assigned Device Management Service Information for a Device](#get-the-assigned-device-management-service-information-for-a-device)
  - [Assign or Unassign Devices to a Device Management Service](#assign-or-unassign-devices-to-a-device-management-service)
  - [Get Organization Device Activity Information](#get-organization-device-activity-information)
- [Users](#users)
  - [Get Users](#get-users)
  - [Get User Information](#get-user-information)
- [UserGroups](#usergroups)
  - [Get User Groups](#get-user-groups)
  - [Get User Group Information](#get-user-group-information)
  - [Get User IDs for a User Group](#get-user-ids-for-a-user-group)
- [Apps and Packages](#apps-and-packages)
  - [Get Apps](#get-apps)
  - [Get App Information](#get-app-information)
  - [Get Packages](#get-packages)
  - [Get Package Information](#get-package-information)
- [Blueprints](#blueprints)
  - [Get Blueprints](#get-blueprints)
  - [Create a Blueprint](#create-a-blueprint)
  - [Get Blueprint Information](#get-blueprint-information)
  - [Update a Blueprint](#update-a-blueprint)
  - [Delete a Blueprint](#delete-a-blueprint)
  - [Get App IDs for a Blueprint](#get-app-ids-for-a-blueprint)
  - [Add Apps to a Blueprint](#add-apps-to-a-blueprint)
  - [Remove Apps from a Blueprint](#remove-apps-from-a-blueprint)
  - [Get Configuration IDs for a Blueprint](#get-configuration-ids-for-a-blueprint)
  - [Add Configurations to a Blueprint](#add-configurations-to-a-blueprint)
  - [Remove Configurations from a Blueprint](#remove-configurations-from-a-blueprint)
  - [Get Package IDs for a Blueprint](#get-package-ids-for-a-blueprint)
  - [Add Packages to a Blueprint](#add-packages-to-a-blueprint)
  - [Remove Packages from a Blueprint](#remove-packages-from-a-blueprint)
  - [Get Device IDs for a Blueprint](#get-device-ids-for-a-blueprint)
  - [Add Devices to a Blueprint](#add-devices-to-a-blueprint)
  - [Remove Devices from a Blueprint](#remove-devices-from-a-blueprint)
  - [Get User IDs for a Blueprint](#get-user-ids-for-a-blueprint)
  - [Add Users to a Blueprint](#add-users-to-a-blueprint)
  - [Remove Users from a Blueprint](#remove-users-from-a-blueprint)
  - [Get User Group IDs for a Blueprint](#get-user-group-ids-for-a-blueprint)
  - [Add User Groups to a Blueprint](#add-user-groups-to-a-blueprint)
  - [Remove User Groups from a Blueprint](#remove-user-groups-from-a-blueprint)
- [Configurations](#configurations)
  - [Get Configurations](#get-configurations)
  - [Create a Configuration](#create-a-configuration)
  - [Get Configuration Information](#get-configuration-information)
  - [Update a Configuration](#update-a-configuration)
  - [Delete a Configuration](#delete-a-configuration)
- [Audit Events](#audit-events)
  - [Get Audit Events](#get-audit-events)

---

## Devices

### Get Organization Devices

Get a list of devices in an organization that enroll using Automated Device Enrollment.

```http
GET https://api-business.apple.com/v1/orgDevices
```

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[orgDevices]` | `[string]` | No | The fields to return for included related types. Allowed: `serialNumber`, `addedToOrgDateTime`, `releasedFromOrgDateTime`, `releaserId`, `releaserEntityType`, `updatedDateTime`, `deviceModel`, `productFamily`, `productType`, `deviceCapacity`, `partNumber`, `orderNumber`, `color`, `status`, `orderDateTime`, `imei`, `meid`, `eid`, `purchaseSourceId`, `purchaseSourceType`, `wifiMacAddress`, `bluetoothMacAddress`, `ethernetMacAddress`, `assignedServer`, `appleCareCoverage` |
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `OrgDevicesResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get Device Information

Get information about a device in an organization.

```http
GET https://api-business.apple.com/v1/orgDevices/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[orgDevices]` | `[string]` | No | The fields to return for included related types. Allowed: `serialNumber`, `addedToOrgDateTime`, `releasedFromOrgDateTime`, `releaserId`, `releaserEntityType`, `updatedDateTime`, `deviceModel`, `productFamily`, `productType`, `deviceCapacity`, `partNumber`, `orderNumber`, `color`, `status`, `orderDateTime`, `imei`, `meid`, `eid`, `purchaseSourceId`, `purchaseSourceType`, `wifiMacAddress`, `bluetoothMacAddress`, `ethernetMacAddress`, `assignedServer`, `appleCareCoverage` |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `OrgDeviceResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get AppleCare coverage Information for a Device

Get a list of AppleCare coverage resources for an organization device.

```http
GET https://api-business.apple.com/v1/orgDevices/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. For example, the device’s serial number. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[appleCareCoverage]` | `[string]` | No | The fields to return for included related types. Allowed: `status`, `paymentType`, `description`, `agreementNumber`, `startDateTime`, `endDateTime`, `isRenewable`, `isCanceled`, `contractCancelDateTime` |
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `AppleCareCoverageResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get Devices Enrolled in Apple Device Management Service

Get a list of devices enrolled in Apple Device Management service.

```http
GET https://api-business.apple.com/v1/mdmDevices
```

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[mdmDevices]` | `[string]` | No | The fields to return for included related types. Allowed: `serialNumber`, `deviceName`, `productFamily`, `enrolledUserId`, `details` |
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `MdmDevicesResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get Details for a Device Enrolled in Apple Device Management Service

Get detailed information about a device enrolled in Apple Device Management service.

```http
GET https://api-business.apple.com/v1/mdmDevices/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[mdmDeviceDetails]` | `[string]` | No | The fields to return for included related types. Allowed: `serialNumber`, `deviceName`, `deviceModel`, `osVersion`, `platform`, `imei`, `meid`, `wifiMacAddress`, `bluetoothMacAddress`, `ethernetMacAddress`, `lastCheckInDateTime`, `isFirewallEnabled`, `isFileVaultEnabled`, `storageFreeCapacity`, `storageTotalCapacity`, `deviceLockStatus`, `deviceEraseStatus`, `lostModeStatus` |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `MdmDeviceDetailResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

## Device Management Services

### Get Device Management Services

Get a list of device management services in an organization.

```http
GET https://api-business.apple.com/v1/mdmServers
```

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[mdmServers]` | `[string]` | No | The fields to return for included related types. Allowed: `serverName`, `serverType`, `createdDateTime`, `updatedDateTime`, `devices` |
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `MdmServersResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get the Device Serial Numbers for a Device Management Service

Get a list of device serial numbers assigned to a device management service.

```http
GET https://api-business.apple.com/v1/mdmServers/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `MdmServerDevicesLinkagesResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get the Assigned Device Management Service ID for a Device

Get the assigned device management service ID information for a device.

```http
GET https://api-business.apple.com/v1/orgDevices/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `OrgDeviceAssignedServerLinkageResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get the Assigned Device Management Service Information for a Device

Get the assigned device management service information for a device.

```http
GET https://api-business.apple.com/v1/orgDevices/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[mdmServers]` | `[string]` | No | The fields to return for included related types. Allowed: `serverName`, `serverType`, `createdDateTime`, `updatedDateTime`, `devices` |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `MdmServerResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Assign or Unassign Devices to a Device Management Service

Assign or unassign devices to a device management service.

```http
POST https://api-business.apple.com/v1/orgDeviceActivities
```

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | Created | `OrgDeviceActivityResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get Organization Device Activity Information

Get information for an organization device activity that a device management action, such as assign or unassign, creates.

```http
GET https://api-business.apple.com/v1/orgDeviceActivities/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[orgDeviceActivities]` | `[string]` | No | The fields to return for included related types. Allowed: `status`, `subStatus`, `createdDateTime`, `completedDateTime`, `downloadUrl` |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `OrgDeviceActivityResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

## Users

### Get Users

Get a list of users in an organization.

```http
GET https://api-business.apple.com/v1/users
```

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[users]` | `[string]` | No | The fields to return for included related types. Allowed: `firstName`, `lastName`, `middleName`, `status`, `managedAppleAccount`, `isExternalUser`, `roleOuList`, `email`, `employeeNumber`, `costCenter`, `division`, `department`, `jobTitle`, `startDateTime`, `createdDateTime`, `updatedDateTime`, `phoneNumbers` |
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `UsersResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get User Information

Get information about a specific user in an organization.

```http
GET https://api-business.apple.com/v1/users/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[users]` | `[string]` | No | The fields to return for included related types. Allowed: `firstName`, `lastName`, `middleName`, `status`, `managedAppleAccount`, `isExternalUser`, `roleOuList`, `email`, `employeeNumber`, `costCenter`, `division`, `department`, `jobTitle`, `startDateTime`, `createdDateTime`, `updatedDateTime`, `phoneNumbers` |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `UserResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

## UserGroups

### Get User Groups

Get a list of user groups in an organization.

```http
GET https://api-business.apple.com/v1/userGroups
```

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[userGroups]` | `[string]` | No | The fields to return for included related types. Allowed: `ouId`, `name`, `type`, `totalMemberCount`, `createdDateTime`, `updatedDateTime`, `status`, `users` |
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `UserGroupsResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get User Group Information

Get information about a specific user group in an organization.

```http
GET https://api-business.apple.com/v1/userGroups/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[userGroups]` | `[string]` | No | The fields to return for included related types. Allowed: `ouId`, `name`, `type`, `totalMemberCount`, `createdDateTime`, `updatedDateTime`, `status`, `users` |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `UserGroupResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get User IDs for a User Group

Get a list of user IDs for a user group in an organization.

```http
GET https://api-business.apple.com/v1/userGroups/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `UserGroupUsersLinkagesResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

## Apps and Packages

### Get Apps

Get a list of apps for an organization using the built-in device management in Apple Business.

```http
GET https://api-business.apple.com/v1/apps
```

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[apps]` | `[string]` | No | The fields to return for included related types. Allowed: `name`, `bundleId`, `websiteUrl`, `version`, `supportedOS`, `isCustomApp`, `appStoreUrl` |
| `limit` | `integer` | No | The number of resources to return (maximum 1000). |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `AppsResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get App Information

Get information about a specific app for an organization using the built-in device management in Apple Business.

```http
GET https://api-business.apple.com/v1/apps/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[apps]` | `[string]` | No | The fields to return for included related types. Allowed: `name`, `bundleId`, `websiteUrl`, `version`, `supportedOS`, `isCustomApp`, `appStoreUrl` |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `AppResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get Packages

Get a list of packages for an organization using Apple Business’s built-in device management.

```http
GET https://api-business.apple.com/v1/packages
```

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[packages]` | `[string]` | No | The fields to return for included related types. Allowed: `name`, `url`, `hash`, `bundleIds`, `description`, `version`, `createdDateTime`, `updatedDateTime` |
| `limit` | `integer` | No | The number of resources to return (maximum 1000). |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `PackagesResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get Package Information

Get information about a specific package for an organization using Apple Business’s built-in device management.

```http
GET https://api-business.apple.com/v1/packages/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[packages]` | `[string]` | No | The fields to return for included related types. Allowed: `name`, `url`, `hash`, `bundleIds`, `description`, `version`, `createdDateTime`, `updatedDateTime` |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `PackageResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

## Blueprints

### Get Blueprints

Get a list of Blueprints in an organization.

```http
GET https://api-business.apple.com/v1/blueprints
```

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[blueprints]` | `[string]` | No | The fields to return for included related types. Allowed: `name`, `description`, `status`, `createdDateTime`, `updatedDateTime`, `appLicenseDeficient`, `apps`, `packages`, `configurations`, `orgDevices`, `users`, `userGroups` |
| `limit` | `integer` | No | The number of resources to return (maximum 1000). |
| `include` | `[string]` | No | Related resources to include in the response. Possible values: , , , , , . Allowed: `apps`, `packages`, `configurations`, `orgDevices`, `users`, `userGroups` |
| `limit[apps]` | `integer` | No | Maximum number of related apps to include. |
| `limit[configurations]` | `integer` | No | Maximum number of related configurations to include. |
| `limit[packages]` | `integer` | No | Maximum number of related packages to include. |
| `limit[orgDevices]` | `integer` | No | Maximum number of related devices to include. |
| `limit[users]` | `integer` | No | Maximum number of related users to include. |
| `limit[userGroups]` | `integer` | No | Maximum number of related user groups to include. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `BlueprintsResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Create a Blueprint

Create a new Blueprint in an organization.

```http
POST https://api-business.apple.com/v1/blueprints
```

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | Created | `BlueprintResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get Blueprint Information

Get information about a specific Blueprint in an organization.

```http
GET https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[blueprints]` | `[string]` | No | The fields to return for included related types. Allowed: `name`, `description`, `status`, `createdDateTime`, `updatedDateTime`, `appLicenseDeficient`, `apps`, `packages`, `configurations`, `orgDevices`, `users`, `userGroups` |
| `include` | `[string]` | No | Related resources to include in the response. Possible values: , , , , , . Allowed: `apps`, `packages`, `configurations`, `orgDevices`, `users`, `userGroups` |
| `limit[apps]` | `integer` | No | Maximum number of related apps to include. |
| `limit[configurations]` | `integer` | No | Maximum number of related configurations to include. |
| `limit[packages]` | `integer` | No | Maximum number of related packages to include. |
| `limit[orgDevices]` | `integer` | No | Maximum number of related devices to include. |
| `limit[users]` | `integer` | No | Maximum number of related users to include. |
| `limit[userGroups]` | `integer` | No | Maximum number of related user groups to include. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `BlueprintResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Update a Blueprint

Update an existing Blueprint in an organization.

```http
PATCH https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `BlueprintResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Delete a Blueprint

Delete a Blueprint from an organization.

```http
DELETE https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get App IDs for a Blueprint

Get a list of app IDs associated with a Blueprint.

```http
GET https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `BlueprintAppsLinkagesResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Add Apps to a Blueprint

Add apps to a Blueprint.

```http
POST https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Remove Apps from a Blueprint

Remove apps from a Blueprint.

```http
DELETE https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get Configuration IDs for a Blueprint

Get a list of Configuration IDs associated with a Blueprint.

```http
GET https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `BlueprintConfigurationsLinkagesResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Add Configurations to a Blueprint

Add Configurations to a Blueprint.

```http
POST https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Remove Configurations from a Blueprint

Remove Configurations from a Blueprint.

```http
DELETE https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get Package IDs for a Blueprint

Get a list of package IDs associated with a Blueprint.

```http
GET https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `BlueprintPackagesLinkagesResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Add Packages to a Blueprint

Add packages to a Blueprint.

```http
POST https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Remove Packages from a Blueprint

Remove packages from a Blueprint.

```http
DELETE https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get Device IDs for a Blueprint

Get a list of device IDs associated with a Blueprint.

```http
GET https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `BlueprintOrgDevicesLinkagesResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Add Devices to a Blueprint

Add devices to a Blueprint.

```http
POST https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Remove Devices from a Blueprint

Remove devices from a Blueprint.

```http
DELETE https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get User IDs for a Blueprint

Get a list of user IDs associated with a Blueprint.

```http
GET https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `BlueprintUsersLinkagesResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Add Users to a Blueprint

Add users to a Blueprint.

```http
POST https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Remove Users from a Blueprint

Remove users from a Blueprint.

```http
DELETE https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get User Group IDs for a Blueprint

Get a list of user group IDs associated with a Blueprint.

```http
GET https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | `integer` | No | The number of included related resources to return. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `BlueprintUserGroupsLinkagesResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Add User Groups to a Blueprint

Add user groups to a Blueprint.

```http
POST https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Remove User Groups from a Blueprint

Remove user groups from a Blueprint.

```http
DELETE https://api-business.apple.com/v1/blueprints/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

## Configurations

### Get Configurations

Get a list of Configurations in an organization.

```http
GET https://api-business.apple.com/v1/configurations
```

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[configurations]` | `[string]` | No | The fields to return for included related types. Allowed: `type`, `name`, `configuredForPlatforms`, `customSettingsValues`, `createdDateTime`, `updatedDateTime` |
| `limit` | `integer` | No | The number of resources to return (maximum 1000). |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `ConfigurationsResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Create a Configuration

Create a new custom Configuration in an organization.

```http
POST https://api-business.apple.com/v1/configurations
```

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | Created | `ConfigurationResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Get Configuration Information

Get information about a specific Configuration in an organization.

```http
GET https://api-business.apple.com/v1/configurations/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `fields[configurations]` | `[string]` | No | The fields to return for included related types. Allowed: `type`, `name`, `configuredForPlatforms`, `customSettingsValues`, `createdDateTime`, `updatedDateTime` |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `ConfigurationResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Update a Configuration

Update an existing custom Configuration in an organization.

```http
PATCH https://api-business.apple.com/v1/configurations/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `ConfigurationResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

### Delete a Configuration

Delete a Configuration from an organization.

```http
DELETE https://api-business.apple.com/v1/configurations/
```

#### Path Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `id` | `string` | ✓ Yes | The unique identifier for the resource. |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | No Content | `` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  | Not Found | `ErrorResponse` |
|  | Conflict | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

## Audit Events

### Get Audit Events

Get a list of audit events in an organization that satisfies the query criteria.

```http
GET https://api-business.apple.com/v1/auditEvents
```

#### Query Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `filter[startTimestamp]` | `[string]` | ✓ Yes | ISO8601 formatted start timestamp of query time range. |
| `filter[endTimestamp]` | `[string]` | ✓ Yes | ISO8601 formatted end timestamp of query time range. |
| `filter[actorId]` | `[string]` | No | Id of actor of event. Note that only one actor id in query is supported. |
| `filter[subjectId]` | `[string]` | No | Id of subject of event. Note that only one subject id in query is supported. |
| `filter[type]` | `[string]` | No | Type of event. Note that only one type in query is supported. Allowed: `DEVICE_ADDED_TO_ORG`, `DEVICE_REMOVED_FROM_ORG`, `DEVICE_ASSIGNED_TO_SERVER`, `DEVICE_UNASSIGNED_FROM_SERVER`, `SUBJECT_HAS_ICLOUD_STORAGE_PURCHASE_ADDED`, `SUBJECT_HAS_ICLOUD_STORAGE_PURCHASE_REMOVED`, `SUBJECT_HAS_APPLECARE_PURCHASE_ADDED`, `SUBJECT_HAS_APPLECARE_PURCHASE_REMOVED`, `DEVICE_IS_ERASED`, `CONFIG_SETTINGS_CREATED`, `CONFIG_SETTINGS_UPDATED`, `CONFIG_SETTINGS_DELETED`, `COLLECTION_CREATED`, `COLLECTION_UPDATED`, `COLLECTION_DELETED`, `SUBSCRIPTION_CREATED`, `SUBSCRIPTION_UPDATED`, `SUBSCRIPTION_DELETED`, `ACCOUNT_ROLE_LOCATION_CHANGED`, `ACCOUNT_ADDED`, `ACCOUNT_DELETED`, `EXTERNAL_ACCOUNT_ASSOCIATED`, `EXTERNAL_ACCOUNT_DISASSOCIATED`, `DOMAIN_ADDED`, `DOMAIN_REMOVED`, `DOMAIN_VERIFIED`, `API_ACCOUNT_CREATED_WITH_KEY`, `API_ACCOUNT_CREATED_WITHOUT_KEY`, `API_ACCOUNT_DELETED`, `API_ACCOUNT_KEY_REVOKED`, `API_ACCOUNT_KEY_GENERATED`, `API_ACCOUNT_ROLE_LOCATION_CHANGED`, `API_ACCOUNT_NAME_CHANGED` |
| `limit` | `integer` | No | The number of included related resources to return. |
| `fields[auditEvents]` | `[string]` | No |  Allowed: `eventDateTime`, `type`, `category`, `actorType`, `actorId`, `actorName`, `subjectType`, `subjectId`, `subjectName`, `outcome`, `groupId`, `eventDataPropertyKey`, `eventDataDeviceAddedToOrg`, `eventDataDeviceRemovedFromOrg`, `eventDataDeviceAssignedToServer`, `eventDataDeviceIsErased`, `eventDataDeviceUnassignedFromServer`, `eventDataSubjectHasICloudStoragePurchaseAdded`, `eventDataSubjectHasICloudStoragePurchaseRemoved`, `eventDataSubjectHasAppleCarePurchaseAdded`, `eventDataSubjectHasAppleCarePurchaseRemoved`, `eventDataConfigSettingsCreated`, `eventDataConfigSettingsUpdated`, `eventDataConfigSettingsDeleted`, `eventDataCollectionCreated`, `eventDataCollectionUpdated`, `eventDataCollectionDeleted`, `eventDataSubscriptionCreated`, `eventDataSubscriptionUpdated`, `eventDataSubscriptionDeleted`, `eventDataAccountRoleLocationChanged`, `eventDataAccountAdded`, `eventDataAccountDeleted`, `eventDataExternalAccountAssociated`, `eventDataExternalAccountDisassociated`, `eventDataDomainAdded`, `eventDataDomainRemoved`, `eventDataDomainVerified`, `eventDataApiAccountCreatedWithKey`, `eventDataApiAccountCreatedWithoutKey`, `eventDataApiAccountDeleted`, `eventDataApiAccountKeyGenerated`, `eventDataApiAccountKeyRevoked`, `eventDataApiAccountNameChanged`, `eventDataApiAccountRoleLocationChanged` |
| `cursor` | `string` | No |  |

#### Response Codes

| Code | Status | Type |
|------|--------|------|
|  | OK | `AuditEventsResponse` |
|  | Bad Request | `ErrorResponse` |
|  | Unauthorized | `ErrorResponse` |
|  | Forbidden | `ErrorResponse` |
|  |  | `ErrorResponse` |

---

