# MDM gRPC Server Architecture

## Tech Stack
- **Go 1.22+** with ConnectRPC (gRPC-compatible, works with browsers)
- **PostgreSQL** for persistence (users, devices, audit logs)
- **Argon2id** for password hashing
- **JWT** for authentication with RBAC (admin/operator/viewer)
- **Clean Architecture**: domain -> port -> adapter/service

## Directory Structure

```
server/
├── cmd/server/main.go          # Entry point, wiring
├── proto/mdm/v1/               # Protobuf definitions
│   ├── auth.proto
│   ├── device.proto
│   ├── command.proto
│   ├── event.proto
│   ├── vpp.proto
│   ├── user.proto
│   └── audit.proto
├── gen/mdm/v1/                 # Generated Go + Connect code
├── internal/
│   ├── domain/                 # Domain entities
│   ├── port/                   # Interface definitions (ports)
│   ├── config/                 # Configuration loading
│   ├── middleware/              # JWT auth interceptor + RBAC
│   ├── adapter/
│   │   ├── postgres/           # DB repositories
│   │   ├── micromdm/           # MicroMDM HTTP client
│   │   └── vpp/                # Apple VPP client
│   └── service/                # ConnectRPC handlers
│       ├── auth_service.go
│       ├── device_service.go
│       ├── command_service.go  # All 22+ MDM commands
│       ├── event_service.go    # Server streaming
│       ├── event_broker.go     # Fan-out pub/sub
│       ├── webhook.go          # MicroMDM webhook receiver
│       ├── vpp_service.go
│       ├── user_service.go
│       └── audit_service.go
├── db/migrations/              # SQL migrations
├── Dockerfile
├── docker-compose.yml
├── buf.yaml
└── buf.gen.yaml
```

## Services

| Service | Methods | Auth |
|---------|---------|------|
| AuthService | Login, RefreshToken, ChangePassword | Public (Login) |
| DeviceService | ListDevices, GetDevice, SyncDevices, SyncDEPDevices | admin/operator |
| CommandService | 22 command types (Lock, Restart, etc.) | admin/operator (Erase=admin only) |
| EventService | StreamEvents (server streaming) | authenticated |
| VPPService | AssignLicense, RevokeLicense | admin/operator |
| UserService | CRUD | admin only |
| AuditService | ListAuditLogs | admin only |

## Running

```bash
# Development
docker-compose up -d postgres
go run ./cmd/server

# Production
docker-compose up -d
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| LISTEN_ADDR | :8080 | Server listen address |
| INTEGRATION_LISTEN_ADDR | (empty = off) | Listen address of the dispatch-system reservation API (see docs/dispatch-integration.md) |
| INTEGRATION_TRUSTED_PROXIES | loopback + private ranges | CIDRs whose `CF-Connecting-IP` header is trusted for the integration API |
| DATABASE_URL | postgres://mdm:mdm@localhost:5432/mdm?sslmode=disable | PostgreSQL DSN |
| JWT_SECRET | change-me-in-production | JWT signing secret |
| MICROMDM_URL | (required) | MicroMDM server URL |
| MICROMDM_API_KEY | (required) | MicroMDM API key |
| VPP_TOKEN_PATH | (optional) | Path to VPP sToken file |
| WEBHOOK_PATH | /webhook | Webhook endpoint path |
