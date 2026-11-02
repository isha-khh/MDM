# 派發任務系統整合 API（預約公司租賃車輛）

對應《派發任務系統 API 介面規格》§7。派發系統呼叫 MDM，在 MDM 預約、修改、取消公司租賃車輛；預約就是 MDM 裡的一筆租借，網頁上看到的是同一份資料。

> 範圍：本文件涵蓋 §7.1–§7.5（可用性、建立、修改、取消、查詢）。§7.6 的 MDM → 派發系統 webhook 與 §7.7 每日對帳是第二階段，尚未實作。

## 1. 運作方式

- **獨立連接埠**：整合 API 不在網頁用的 `:8080`，而是環境變數 `INTEGRATION_LISTEN_ADDR`（例如 `:8081`）指定的另一個監聽。留空 = 不啟用。這個連接埠只有 `/api/v1/…` 與 `/healthz`，沒有網頁、沒有 JWT 登入。
- **驗證**：`Authorization: Bearer <API key>`。金鑰只存雜湊，明文只在發行當下顯示一次。
- **免核准**：用有效的 API key 建立的預約直接是「已核准」，核准人欄位記為「派發系統」。
- **時間到分鐘**：`startAt`／`endAt` 是帶時區的 ISO 8601（`+08:00`），秒數必須是 0，區間為 `[startAt, endAt)`，前一筆的 `endAt` 等於下一筆的 `startAt` 不算衝突。
- 只有派發系統會呼叫 MDM（Forma／Protrack 是呼叫派發系統）。

## 2. 上線步驟

1. 設定 `INTEGRATION_LISTEN_ADDR=:8081`（root `docker-compose.yml` 已有這個變數），重啟 api。
2. 以系統管理員登入，到「系統管理 → 設定」最下方的「派發系統整合 API」，按「發行 API key」（對應 `POST /api/settings/integration/key`），交給派發系統。**只會顯示一次**；要換就按「輪替 key」（舊的立刻失效）。
3. 在同一張卡片的「資源（車輛）對應」，把資源代號（如 `RENTAL_CAR_01`）對到車輛資產。可預約縣市預設為台南市、高雄市、屏東縣，可以改；暫時不開放用「停用」，不要移除對應（移除後派發系統查不到這輛車已建立的預約）。
4. （建議）填「IP 白名單」（派發系統的出口 IP）；「可自動建立借用人帳號的 email 網域」預設只有 `mail.isha.org.tw`（見 §5）。
5. 該車所屬分類在「分類管理」打開「可預約」：沒開的話，網頁上有人用舊的「借出中就不能借」規則借這輛車時，會被未來的預約擋住。
6. 設定 Cloudflare Tunnel（§3）。

## 3. Cloudflare Tunnel 與 WAF

- Tunnel 的 service 指向整合連接埠（`http://api:8081`，或主機上的 `http://127.0.0.1:8081`）。**只**發佈整合連接埠，不要把 `:8080` 掛出去。
- 經過 tunnel 時，TCP 連線的對端是 `cloudflared`，真正的來源 IP 在 `CF-Connecting-IP`。MDM 只在連線對端落在 `INTEGRATION_TRUSTED_PROXIES`（預設：本機與私有網段）時才相信這個標頭；直接連到連接埠的人無法用假標頭冒充允許清單內的 IP。
- WAF 建議：只放行路徑 `/api/v1/*` 與 `/healthz`；限制方法為 GET／POST／PUT；依來源 IP 或 Cloudflare Access service token 再擋一層；對 `/api/v1/*` 設 rate limit（規格 §7.8 的呼叫頻率限制待雙方確認）。API key 的 IP 白名單是第二道，不取代 WAF。

## 4. API

Base URL：`https://{host}/api/v1`。錯誤一律：

```json
{ "error": { "code": "VEHICLE_UNAVAILABLE", "message": "該時段已被預約", "details": [] } }
```

| HTTP | code | 意義 |
|---|---|---|
| 400 | `VALIDATION_FAILED` | 欄位錯誤（含缺少 `Idempotency-Key`、秒數不為 0、`endAt` 不晚於 `startAt`、缺 `destination.city`） |
| 401 | `UNAUTHORIZED` | 沒帶 key 或 key 無效 |
| 403 | `FORBIDDEN` | key 已停用，或來源 IP 不在允許清單 |
| 404 | `NOT_FOUND` | 預約或資源不存在 |
| 409 | `VEHICLE_UNAVAILABLE` | 時段已被預約，或車輛維修／報廢等無法出借；`details` 列出擋住的預約 |
| 409 | `EXTERNAL_REF_EXISTS` | 這個 `externalRef` 已有未取消的預約；`details.reservationId` 指向它 |
| 409 | `IDEMPOTENCY_KEY_CONFLICT` | 同一個 `Idempotency-Key` 用在內容不同的請求 |
| 422 | `REGION_NOT_SUPPORTED` | 目的地縣市不在可預約範圍（`台` 與 `臺` 視為相同） |
| 422 | `BORROWER_NOT_FOUND` / `BORROWER_AMBIGUOUS` / `BORROWER_INACTIVE` | 借用人無法對應（見 §5） |
| 422 | `RESERVATION_NOT_MODIFIABLE` | 已出車／已還車／已取消，不能修改或取消 |

### 寫入與冪等

`POST`／`PUT` 必須帶 `Idempotency-Key`（1–255 字元）。同一個 key 重送**同樣的請求**，回傳第一次的結果（`Idempotent-Replayed: true`），不會重複預約；同一個 key 搭配不同內容回 409。key 保留 7 天。只有成功的結果會被記住——失敗（例如 409）可以用同一個 key 重試。

### GET /availability

`?serviceType=RENTAL_CAR&from=…&to=…`（`resourceId` 選填）

```json
{ "available": true, "resourceId": "RENTAL_CAR_01", "conflicts": [] }
```

有多輛車時回傳第一輛有空的；都沒空時回傳第一輛與它的 `conflicts`（`reservationId`、`externalRef`、`status`、`startAt`、`endAt`）。車輛維修等不能出借時 `available:false`，並帶 `reason`（如 `ASSET_MAINTENANCE`）。

### POST /reservations

```json
{
  "externalRef": "protrack:P-88",
  "serviceType": "RENTAL_CAR",
  "borrowerEmployeeId": "wang.xm",
  "borrowerEmail": "wang.xm@mail.isha.org.tw",
  "passengers": ["wang.xm", "lee.xh"],
  "startAt": "2026-10-30T09:00:00+08:00",
  "endAt": "2026-10-30T17:00:00+08:00",
  "destination": { "address": "高雄市前鎮區○○路○號", "city": "高雄市", "district": "前鎮區" },
  "note": "派工單 P-88"
}
```

回 `201`，內容是完整預約物件：

```json
{
  "reservationId": "R-1001", "externalRef": "protrack:P-88", "serviceType": "RENTAL_CAR",
  "resourceId": "RENTAL_CAR_01", "status": "RESERVED",
  "borrowerEmployeeId": "wang.xm", "borrowerEmail": "wang.xm@mail.isha.org.tw", "borrowerName": "王小明",
  "passengers": ["wang.xm", "lee.xh"],
  "startAt": "2026-10-30T09:00:00+08:00", "endAt": "2026-10-30T17:00:00+08:00",
  "destination": { "address": "高雄市前鎮區○○路○號", "city": "高雄市", "district": "前鎮區" },
  "note": "派工單 P-88", "createdVia": "api",
  "createdAt": "2026-10-29T10:00:00+08:00", "updatedAt": "2026-10-29T10:00:00+08:00"
}
```

相對於規格新增（皆為選填，向下相容）：`borrowerEmail`、`borrowerName`、`resourceId`（指定車輛；不填則在符合縣市範圍的車輛中取第一輛有空的）。`passengers` 原樣保存（AD 帳號），MDM 不去對應帳號。

### PUT /reservations/{id}

部分更新：只送要改的欄位（`startAt`、`endAt`、`borrowerEmployeeId`／`borrowerEmail`／`borrowerName`、`passengers`、`destination`、`note`）。只能修改「尚未出車」（`RESERVED`）的預約。`externalRef`、`serviceType`、`resourceId` 不可變更。改時段會重新檢查衝突（不會跟自己衝突）；改 `destination` 會重新檢查縣市。

### POST /reservations/{id}/cancel

body `{"reason": "..."}`（可省略）。取消是狀態變更，不刪除。已取消的再取消回 200 且不改內容；已出車／已還車回 422。

### GET /reservations/{id}

### GET /reservations?externalRef=protrack:P-88

建立逾時時用來確認是否已建立。同一個 `externalRef` 可能有已取消的舊預約，未取消的排在最前面。

### GET /reservations?from=…&to=…[&resourceId=…]

列出時段內**所有**預約（含 MDM 網頁上手動建立的、已取消的；`createdVia` 為 `web` 或 `api`），供每日對帳。網頁上舊式「整天」租借以其整天區間（00:00 到隔天 00:00）呈現。

### 狀態

| MDM 租借狀態 | `status` |
|---|---|
| 待核准、已核准 | `RESERVED` |
| 借出中、待核對 | `IN_USE` |
| 已歸還 | `RETURNED` |
| 已取消、已拒絕 | `CANCELLED` |

## 5. 借用人如何對應

MDM 的 `username` 不一定等於 AD 帳號：只有經 SSO 建立／綁定的帳號，`username` 才是 AD 帳號；本機帳號的名字沒有意義。所以：

1. 有 `borrowerEmail`：以 email（不分大小寫）找帳號。找到一個就用；找到多個回 `BORROWER_AMBIGUOUS`；帳號已停用回 `BORROWER_INACTIVE`。
2. 沒找到，且有 `borrowerEmployeeId`：只比對**已綁定 SSO** 的帳號的 `username`（若同時給了 email，帳號的 email 須相符）。
3. 還是沒有：
   - 有 `borrowerEmail`，且網域在該 API key 的「可自動建立帳號的網域」清單（預設 `mail.isha.org.tw`）→ **自動建立**一個沒有密碼的帳號（`username` 用 `borrowerEmployeeId`，沒有就用 email 前段；撞名加後綴；顯示名稱用 `borrowerName`，沒有就用 email 前段）。之後此人用 SSO 登入時，會以 email 對到這個帳號。預約失敗（衝突等）時不會留下帳號。
   - 否則回 `BORROWER_NOT_FOUND`。**不用名字猜。**
4. 回應的 `borrowerEmployeeId`：優先是派發系統送來的值；否則 SSO 帳號用 `username`；其餘為 `null`。要認人請以 `borrowerEmail` 為準。

## 6. MDM → 派發系統 webhook（§7.6）

行政人員在 MDM 網頁上動了預約，MDM 就通知派發系統。**透過這份 API 做的異動不會回報**（派發系統自己知道）。

### 設定

兩個環境變數都設了才會啟用（只設一個會在啟動紀錄警告）：

| 變數 | 說明 |
|---|---|
| `DISPATCH_WEBHOOK_URL` | 派發系統的接收端點，如 `https://dispatch.example.com/integrations/mdm/events` |
| `DISPATCH_WEBHOOK_SECRET` | 與派發系統約定的簽章金鑰（建議 32 字元以上的隨機值；不要含 ` #`）。**只放環境變數，不進程式碼、不進資料庫** |

設定畫面（整合 API 卡片）會顯示 webhook 是否啟用、等待中與已放棄的事件數、最近一次錯誤，並可把已放棄的事件重新排入佇列。

### 什麼時候送

只針對對應到整合資源（如 `RENTAL_CAR_01`）的租借，並且是動作做完以後「這筆預約的內容有變」才送：

| 網頁上的動作 | 事件 |
|---|---|
| 新增租借（含舊的整天租借） | `reservation.created` |
| 時間、借用人、乘客、車輛改變（含續借核准） | `reservation.changed` |
| 拒絕、刪除 | `reservation.cancelled`（刪除的以 `status: CANCELLED` 回報） |
| 交付（出車）、歸還 | `reservation.status_changed` |
| 核准、逐日回報、存查 | 不送（預約內容沒變） |

事件內容照規格 §7.6：`eventId`（`mdm-evt-yyyymmdd-nnnnnn`）、`eventType`、`occurredAt`、`reservationId`、`externalRef`（MDM 自己建立的為 `null`）、`changedBy`（操作者的 MDM 帳號）、`note`（續借核准時為審核備註）、`reservation`、`previous`（`created` 為 `null`）。`reservation` 另外帶 `borrowerEmail`、`borrowerName`。網頁上舊式的整天租借時間為當日 00:00 到隔天 00:00。

### 簽章與重送

- `X-Timestamp`：送出當下的 Unix 秒數；`X-Signature`：`sha256=` + `HMAC-SHA256(secret, "{X-Timestamp}.{原始 body}")` 的小寫 hex。重送時時間戳與簽章重新計算，**body 與 `eventId` 不變**。
- 回 200 視為成功；回 400 不重試（事件本身有問題，標為失敗）；其他（含 401、逾時、5xx）依 1、2、4、8、16 分鐘重試，共 5 次，之後標為失敗。401 也重試是因為 secret 或主機時間修好後可以自然恢復。
- 同一筆預約的事件依發生順序送：前一個還沒送成功，後一個等著，不會超前。不同預約之間彼此獨立。
- 事件先寫進資料庫佇列（`integration_webhook_outbox`）再由背景工作送，所以 MDM 重啟或派發系統暫時不通都不會掉事件；已送成功的保留 30 天。

## 7. 呼叫紀錄（查「派發系統到底有沒有呼叫、被什麼擋掉」）

整合 API 的每一次呼叫（`/healthz` 除外）都會記錄，兩個地方：

- **伺服器 log**（`docker compose logs api | grep integration-api`），每次一行，長期保留（依 docker log 設定）：

  ```
  [integration-api] POST /api/v1/reservations 409 VEHICLE_UNAVAILABLE 4ms client=派發系統 ip=203.0.113.7
  [integration-api] GET /api/v1/reservations 401 UNAUTHORIZED 0ms client=- ip=198.51.100.9 key=mdm_oldKey…
  ```

  內容只有方法、路徑、狀態碼、錯誤代碼、耗時、呼叫端、來源 IP；重送回放的會加 `replayed`；用了無法辨識的 key（例如輪替前的舊 key）時，只記它的前 10 個字元，用來判斷用的是哪一把。**不含 API key、請求內容、回應內容。**
- **設定畫面最下方「最近的 API 呼叫」**：最近 200 筆，只放在伺服器記憶體（重啟後清空），只有系統管理員看得到。點一列展開成兩欄：左邊「傳入參數」（請求、呼叫端、來源 IP、Idempotency-Key、請求內容），右邊「傳出參數」（HTTP 狀態、錯誤代碼與訊息、耗時、回應內容）。每筆的請求與回應內容各只保留前 8 KB；內容裡會有借用人 email、目的地等個資，所以不寫進 log。

判讀：

| 看到 | 意思 |
|---|---|
| 完全沒有那一筆 | 請求沒到 MDM（tunnel、WAF 或網址的問題） |
| `401`，`client=-` | 沒帶 key 或 key 無效；`key=` 的前綴可對照設定畫面顯示的目前 key 前綴，不同就是用了舊 key |
| `403`，有 `client=` | key 對，但已停用，或來源 IP 不在白名單（看 `ip=`） |
| `409` / `422` / `400` | 請求到了、驗證過了，業務規則不允許；錯誤代碼與訊息說明原因 |

## 8. 尚未實作

- §7.7 每日對帳：由派發系統端用 `GET /reservations?from=…&to=…` 比對，MDM 不需再做。
- 一張網頁租借單同時租兩輛不同的整合資源車輛時，只回報第一輛（預約編號 `R-{rental_number}` 對兩輛是同一個）；目前只有一輛車，不影響。
