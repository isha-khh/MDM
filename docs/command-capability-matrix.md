# MDM Command Capability Matrix

> Single source of truth for "which command works on which device".
> The runtime version of this lives in
> [`src/mdm.web/src/lib/mdmCommands.ts`](../src/mdm.web/src/lib/mdmCommands.ts) —
> any change here must be mirrored there and vice versa.

## Why this exists

Each Apple MDM command has different rules:

- **Platform** — `EnableLostMode` is iOS/iPadOS only; `ClearPasscode` doesn't work on macOS.
- **Supervised mode** — `RestartDevice` / `ShutDownDevice` require Supervised; non-supervised devices ignore the command.
- **Required parameters** — macOS `DeviceLock` needs a 6-digit PIN; iOS doesn't.
- **State preconditions** — `DeviceLocation` only works while the device is in Lost Mode.

Before we encoded this in `mdmCommands.ts`, the UI sent everything to every device and let Apple reject it. Errors came back as opaque `ErrorChain` entries. Now incompatible commands are disabled at the button level with a tooltip explaining why.

## The matrix

Legend:

- ✓ supported
- ✗ not supported (Apple rejects)
- — N/A (command doesn't make sense for this platform)
- ⚠ supported with caveat — see Required Params column

| 中文標籤 | Apple Request Type | iOS / iPadOS | macOS | tvOS | Supervised? | Lost Mode? | Required Params |
|---------|--------------------|--------------|-------|------|-------------|-----------|-----------------|
| 推播 | (APNs push, not a command) | ✓ | ✓ | ✓ | — | — | — |
| 鎖定裝置 | `DeviceLock` | ✓ | ⚠ | ✗ | No | — | **macOS: 6-digit PIN** (required); iOS: optional Message |
| 重新啟動 | `RestartDevice` | iOS 10.3+ | macOS 10.13+ | tvOS 10.2+ | **Yes** | — | — |
| 關機 | `ShutDownDevice` | iOS 10.3+ | macOS 10.13+ | ✗ | **Yes** | — | — |
| 清除密碼 | `ClearPasscode` | ✓ | ✗ | ✗ | No | — | `UnlockToken` (auto-injected by MicroMDM from device's TokenUpdate) |
| 啟用遺失模式 | `EnableLostMode` | ✓ | ✗ | ✗ | **Yes** | — | Message + PhoneNumber + Footnote (≥1) |
| 關閉遺失模式 | `DisableLostMode` | ✓ | ✗ | ✗ | **Yes** | — | — |
| 定位 | `DeviceLocation` | ✓ | ✗ | ✗ | **Yes** | **Required** | — |
| 播放聲音 | `PlayLostModeSound` | ✓ | ✗ | ✗ | **Yes** | **Required** | — |
| 安裝 App | `InstallApplication` | ✓ | ✓ | tvOS partial | No | — | iTunes Store ID (VPP) or ManifestURL (Enterprise) |
| 更新 App | `InstallApplication` (with update flag) | ✓ | ✓ | tvOS partial | No | — | App already installed |
| 移除 App | `RemoveApplication` | ⚠ Supervised | ✓ | — | iOS: **Yes** for unmanaged | — | Bundle ID |
| 清除裝置 | `EraseDevice` | ✓ | ⚠ | ✓ | No | — | **macOS 11+: 6-digit PIN** required; iOS: optional PIN |

## Notes

### macOS DeviceLock PIN

macOS uses the PIN as a recovery PIN — the Mac reboots into a locked state and prompts for this PIN. Without it, the command fails with `MCInstallationErrorDomain` or `MCMDMErrorDomain`. The UI now forces a 6-digit PIN dialog before sending.

### macOS EraseDevice PIN

Since macOS 11, EraseDevice requires a 6-digit PIN. Apple Silicon Macs additionally require an `ObliterationBehavior` field — MicroMDM defaults this if absent. Currently the UI sends only `PIN`; if Apple Silicon erase issues arise we'll need to expose `ObliterationBehavior` too.

### ClearPasscode and UnlockToken

`ClearPasscode` needs an `UnlockToken` that the device gave the MDM server in its initial `TokenUpdate` check-in. MicroMDM stores this internally and auto-injects it when the command is queued, so our `clearPasscode` RPC just sends the bare `RequestType`. If a device was migrated from another MDM, the UnlockToken may not be present and the command will fail — re-enroll the device.

### Lost Mode chain (iOS only)

The Lost Mode commands form a strict state machine:

```
[enrolled, supervised] --EnableLostMode--> [lost mode enabled]
                                              |
                          .-------------------+-------------------.
                          |                                       |
                  DeviceLocation                          PlayLostModeSound
                          |                                       |
                          '------------ DisableLostMode -----------'
                                          |
                                          v
                              [enrolled, supervised]
```

`DeviceLocation` and `PlayLostModeSound` are disabled at the button level unless `is_lost_mode = true` on the device.

### Supervised mode detection

We read `is_supervised` from the device record. This is populated when MicroMDM acks a `DeviceInformation` query containing `IsSupervised`. If a device is supervised but `is_supervised = false` in our DB (e.g. never queried), commands requiring Supervised will be wrongly blocked — trigger a sync to refresh.

### Platform inference

We infer platform from `device.model`:

- `iPad*` → `ipados`
- `iPhone*` / `iPod*` → `ios`
- `MacBook*` / `iMac*` / `Mac mini` / `Mac Studio` / `Mac Pro` → `macos`
- `AppleTV` / `Apple TV` → `tvos`
- `Watch*` → `watchos`

Unknown model → `unknown` platform → all platform-gated checks pass (we lean permissive when we don't know). Trigger a `DeviceInformation` sync to populate the model.

## How to add a new command

1. Add the spec to `COMMAND_SPECS` in `src/mdm.web/src/lib/mdmCommands.ts`.
2. Add the corresponding icon in `COMMAND_ICONS` map in `DeviceDetail.tsx`.
3. If the command needs a parameter dialog, declare `dialog: "<dialogKey>"` and `dialogRequiredOn` if conditional. Wire up a new state hook + modal in `DeviceDetail.tsx`.
4. Update this matrix table.
5. Add backend support (proto + service method + MicroMDM payload) if not already present.

## Reference

- Apple Device Management API: <https://developer.apple.com/documentation/devicemanagement>
- MicroMDM v1 commands: <https://github.com/micromdm/micromdm/wiki/REST-API>
