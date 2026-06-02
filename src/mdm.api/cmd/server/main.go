package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/cors"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/anthropics/mdm-server/gen/mdm/v1/mdmv1connect"
	"github.com/anthropics/mdm-server/internal/adapter/abm"
	"github.com/anthropics/mdm-server/internal/adapter/micromdm"
	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	smtpAdapter "github.com/anthropics/mdm-server/internal/adapter/smtp"
	"github.com/anthropics/mdm-server/internal/adapter/vpp"
	"github.com/anthropics/mdm-server/internal/config"
	"github.com/anthropics/mdm-server/internal/controller"
	"github.com/anthropics/mdm-server/internal/db"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/port"
	"github.com/anthropics/mdm-server/internal/service"

	_ "github.com/anthropics/mdm-server/docs"
	httpSwagger "github.com/swaggo/http-swagger"
)

// @title MDM Server API
// @version 2.0
// @description 行動裝置管理系統 API — 裝置管控、資產管理、借用流程、App 派發
// @host localhost:8080
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	log.Println("[startup] === MDM server starting (code version: 2026-03-25-v2) ===")
	cfg := config.Load()

	// Database
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}

	runMigrations(pool)

	go backfillAppIcons(pool)

	// Adapters
	mdmClient := micromdm.NewClient(cfg.MicroMDMURL, cfg.MicroMDMKey)
	vppClient, err := vpp.NewClient(cfg.VPPTokenPath)
	if err != nil {
		log.Printf("VPP client not configured: %v", err)
	}

	// Repositories
	userRepo := postgres.NewUserRepo(pool)
	deviceRepo := postgres.NewDeviceRepo(pool)
	auditRepo := postgres.NewAuditRepo(pool)
	assetRepo := postgres.NewAssetRepo(pool)
	custodyRepo := postgres.NewCustodyRepo(pool)
	appRepo := postgres.NewAppRepo(pool)
	rentalRepo := postgres.NewRentalRepo(pool)
	categoryRepo := postgres.NewCategoryRepo(pool)
	profileRepo := postgres.NewProfileRepo(pool)
	permissionRepo := postgres.NewPermissionRepo(pool)
	notificationRepo := postgres.NewNotificationRepo(pool)
	inventoryRepo := postgres.NewInventoryRepo(pool)
	mailSettingsRepo := postgres.NewMailSettingsRepo(pool)

	// Auth helper (module-level permission checks)
	authHelper := middleware.NewAuthHelper(cfg.JWTSecret, permissionRepo)

	// Notification service (email + audit trail).
	// The sender starts with env-based config and is hot-reloaded from DB
	// both at boot (below) and whenever /api/settings/mail is updated.
	smtpSender := smtpAdapter.NewSender(cfg.SMTP)
	if ms, err := mailSettingsRepo.Get(context.Background()); err == nil && ms.SMTPEnabled {
		smtpSender.SetConfig(config.SMTPConfig{
			Host:     ms.SMTPHost,
			Port:     ms.SMTPPort,
			Username: ms.SMTPUsername,
			Password: ms.SMTPPassword,
			From:     ms.SMTPFrom,
			FromName: ms.SMTPFromName,
			TLS:      ms.SMTPTLS,
		})
	}
	var emailSender port.EmailSender = smtpSender
	notifySvc := service.NewNotifyService(emailSender, notificationRepo, userRepo)

	// Event broker
	broker := service.NewEventBroker()

	// ConnectRPC services
	authSvc := service.NewAuthService(userRepo, cfg.JWTSecret)
	deviceSvc := service.NewDeviceService(mdmClient, deviceRepo, auditRepo)
	commandSvc := service.NewCommandService(mdmClient, vppClient, auditRepo, broker, assetRepo, deviceRepo)
	eventSvc := service.NewEventService(broker)
	vppSvc := service.NewVPPService(vppClient)
	userSvc := service.NewUserService(userRepo)
	auditSvc := service.NewAuditService(auditRepo)

	interceptors := connect.WithInterceptors(middleware.NewAuthInterceptor(cfg.JWTSecret))

	mux := http.NewServeMux()

	// Register ConnectRPC services
	path, handler := mdmv1connect.NewAuthServiceHandler(authSvc, interceptors)
	mux.Handle(path, handler)
	path, handler = mdmv1connect.NewDeviceServiceHandler(deviceSvc, interceptors)
	mux.Handle(path, handler)
	path, handler = mdmv1connect.NewCommandServiceHandler(commandSvc, interceptors)
	mux.Handle(path, handler)
	path, handler = mdmv1connect.NewEventServiceHandler(eventSvc)
	mux.Handle(path, handler)
	path, handler = mdmv1connect.NewVPPServiceHandler(vppSvc, interceptors)
	mux.Handle(path, handler)
	path, handler = mdmv1connect.NewUserServiceHandler(userSvc, interceptors)
	mux.Handle(path, handler)
	path, handler = mdmv1connect.NewAuditServiceHandler(auditSvc, interceptors)
	mux.Handle(path, handler)

	// Webhook endpoint (no auth — MicroMDM calls this)
	webhookHandler := service.NewWebhookHandler(broker, deviceRepo, mdmClient)
	mux.Handle(cfg.WebhookPath, webhookHandler)

	// SocketIO relay
	if cfg.WebSocketURL != "" {
		relay := service.NewSocketIORelay(cfg.WebSocketURL, cfg.MicroMDMKey, webhookHandler)
		relay.Start()
	}

	// DEP auto-assignment scheduler — only starts if explicitly enabled AND
	// the ABM credentials are present. Master switch DEP_AUTO_ASSIGN defaults
	// to false so this won't fire on a fresh deploy without operator opt-in.
	// We hoist the variable so the device controller can expose a "run now"
	// button (admin trigger when not wanting to wait for the next tick).
	var depScheduler *service.DEPScheduler
	if cfg.DEPAutoAssign {
		if cfg.ABMKeyPath == "" || cfg.ABMClientID == "" || cfg.ABMKeyID == "" {
			log.Printf("[dep-scheduler] disabled: DEP_AUTO_ASSIGN=true but ABM_KEY_PATH/CLIENT_ID/KEY_ID missing")
		} else {
			abmClient, err := abm.NewClient(abm.Config{
				KeyPath:  cfg.ABMKeyPath,
				ClientID: cfg.ABMClientID,
				TeamID:   cfg.ABMTeamID,
				KeyID:    cfg.ABMKeyID,
				Scope:    cfg.ABMScope,
			})
			if err != nil {
				log.Printf("[dep-scheduler] disabled: ABM client init: %v", err)
			} else {
				depRepo := postgres.NewDEPAssignmentRepo(pool)
				depScheduler = service.NewDEPScheduler(abmClient, mdmClient, depRepo, cfg.DEPTemplateDir, cfg.DEPPollInterval)
				depScheduler.Start(context.Background())
				log.Printf("[dep-scheduler] enabled, polling every %s, templates in %s", cfg.DEPPollInterval, cfg.DEPTemplateDir)
			}
		}
	} else {
		log.Printf("[dep-scheduler] disabled (DEP_AUTO_ASSIGN=false)")
	}

	// Process pending app commands when device acknowledges
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ch := broker.Subscribe(ctx)
		for evt := range ch {
			if evt.EventType != "acknowledge" || evt.CommandUUID == "" {
				continue
			}
			pending, err := appRepo.GetPendingCommand(context.Background(), evt.CommandUUID)
			if err != nil {
				continue
			}
			if evt.Status == "Acknowledged" {
				if pending.Action == "install" {
					appRepo.AddDeviceApp(context.Background(), pending.DeviceUdid, pending.AppID)
					log.Printf("[pending-app] install confirmed: udid=%s app=%s cmd=%s", pending.DeviceUdid, pending.AppID, evt.CommandUUID)
				} else if pending.Action == "uninstall" {
					appRepo.RemoveDeviceApp(context.Background(), pending.DeviceUdid, pending.AppID)
					log.Printf("[pending-app] uninstall confirmed: udid=%s app=%s cmd=%s", pending.DeviceUdid, pending.AppID, evt.CommandUUID)
				}
			} else {
				log.Printf("[pending-app] command failed: action=%s udid=%s app=%s status=%s cmd=%s",
					pending.Action, pending.DeviceUdid, pending.AppID, evt.Status, evt.CommandUUID)
			}
			appRepo.DeletePendingCommand(context.Background(), evt.CommandUUID)
		}
	}()

	// Daily overdue rental check
	go func() {
		for {
			// Run at startup then every 24 hours
			overdueRentals, err := rentalRepo.ListOverdue(context.Background())
			if err != nil {
				log.Printf("[overdue-check] query error: %v", err)
			} else if len(overdueRentals) > 0 {
				log.Printf("[overdue-check] found %d overdue rental groups", len(overdueRentals))
				for _, rl := range overdueRentals {
					overdueDays := int(time.Since(*rl.ExpectedReturn).Hours() / 24)
					data := service.RentalNotifyData{
						RentalNumber: rl.RentalNumber,
						BorrowerName: rl.BorrowerName,
						OverdueDays:  overdueDays,
					}
					if rl.ExpectedReturn != nil {
						data.ExpectedReturn = rl.ExpectedReturn.Format("2006-01-02")
					}
					// Gather device names
					allRentals, _ := rentalRepo.List(context.Background(), "active", "", false)
					for _, r := range allRentals {
						if r.RentalNumber == rl.RentalNumber {
							name := r.DeviceName
							if name == "" {
								name = r.DeviceSerial
							}
							data.DeviceNames = append(data.DeviceNames, name)
						}
					}

					// Notify borrower
					borrower, err := userRepo.GetByID(context.Background(), rl.BorrowerID)
					if err == nil && borrower.Email != "" {
						notifySvc.SendRentalOverdue(context.Background(), data, borrower.Email)
					}
					// Notify custodian
					if rl.CustodianID != nil && *rl.CustodianID != "" {
						custodian, err := userRepo.GetByID(context.Background(), *rl.CustodianID)
						if err == nil && custodian.Email != "" && (borrower == nil || custodian.Email != borrower.Email) {
							notifySvc.SendRentalOverdue(context.Background(), data, custodian.Email)
						}
					}
				}
			}
			time.Sleep(24 * time.Hour)
		}
	}()

	// Swagger UI
	mux.Handle("/swagger/", httpSwagger.WrapHandler)

	// Register REST controllers
	controller.RegisterAll(mux,
		controller.NewSystemController(pool, cfg),
		controller.NewAuthController(userRepo, authHelper, cfg.JWTSecret),
		// Wrap *DEPScheduler in an explicit interface var so the controller's
		// `if scheduler == nil` check works. Passing the concrete *Scheduler
		// directly would produce a typed-nil interface (interface != nil but
		// underlying pointer == nil) → silent panic on method call.
		controller.NewDeviceController(deviceRepo, mdmClient, authHelper, depSchedulerRunner(depScheduler)),
		controller.NewAssetController(assetRepo, auditRepo, custodyRepo, userRepo, categoryRepo, authHelper),
		controller.NewInventoryController(inventoryRepo, auditRepo, authHelper),
		controller.NewRentalController(rentalRepo, assetRepo, userRepo, notifySvc, authHelper),
		controller.NewAppController(appRepo, deviceRepo, mdmClient, vppClient, auditRepo, authHelper),
		controller.NewUserController(userRepo, permissionRepo, authHelper),
		controller.NewCategoryController(categoryRepo, authHelper),
		controller.NewProfileController(profileRepo, authHelper),
		controller.NewNotificationController(notificationRepo, authHelper),
		controller.NewSettingsController(mailSettingsRepo, smtpSender, authHelper),
	)

	// CORS
	allowedOrigins := []string{"http://localhost:5173", "http://localhost:3000"}
	if env := os.Getenv("CORS_ORIGINS"); env != "" {
		allowedOrigins = strings.Split(env, ",")
	}
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "Connect-Protocol-Version"},
		AllowCredentials: true,
	}).Handler(h2c.NewHandler(mux, &http2.Server{}))

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: corsHandler,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		srv.Shutdown(context.Background())
	}()

	log.Printf("MDM server listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

func runMigrations(pool *pgxpool.Pool) {
	ctx := context.Background()
	for i, sql := range []string{db.MigrationSQL, db.Migration002SQL, db.Migration003SQL, db.Migration004SQL, db.Migration005SQL, db.Migration006SQL, db.Migration007SQL, db.Migration008SQL, db.Migration009SQL, db.Migration010SQL, db.Migration011SQL, db.Migration012SQL, db.Migration013SQL, db.Migration014SQL, db.Migration015SQL, db.Migration016SQL, db.Migration017SQL, db.Migration018SQL, db.Migration019SQL, db.Migration020SQL} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			log.Printf("migration %d: %v (may already be applied)", i+1, err)
		} else {
			log.Printf("migration %d: applied", i+1)
		}
	}
}

func backfillAppIcons(pool *pgxpool.Pool) {
	ctx := context.Background()
	rows, err := pool.Query(ctx,
		`SELECT id, bundle_id, itunes_store_id FROM managed_apps WHERE icon_url = '' AND app_type = 'vpp' AND bundle_id != ''`)
	if err != nil {
		log.Printf("backfill icons query: %v", err)
		return
	}
	defer rows.Close()

	type appInfo struct{ id, bundleID, itunesID string }
	var apps []appInfo
	for rows.Next() {
		var a appInfo
		rows.Scan(&a.id, &a.bundleID, &a.itunesID)
		apps = append(apps, a)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	for _, a := range apps {
		lookupURL := fmt.Sprintf("https://itunes.apple.com/lookup?bundleId=%s&country=tw", a.bundleID)
		resp, err := client.Get(lookupURL)
		if err != nil {
			log.Printf("backfill icon %s: %v", a.bundleID, err)
			continue
		}
		var result struct {
			ResultCount int `json:"resultCount"`
			Results     []struct {
				ArtworkUrl512 string `json:"artworkUrl512"`
				ArtworkUrl100 string `json:"artworkUrl100"`
				TrackID       int    `json:"trackId"`
			} `json:"results"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if result.ResultCount == 0 {
			continue
		}
		r := result.Results[0]
		iconURL := r.ArtworkUrl512
		if iconURL == "" {
			iconURL = r.ArtworkUrl100
		}
		if iconURL == "" {
			continue
		}
		if a.itunesID == "" && r.TrackID > 0 {
			pool.Exec(ctx, `UPDATE managed_apps SET icon_url=$1, itunes_store_id=$2, updated_at=now() WHERE id=$3`,
				iconURL, fmt.Sprint(r.TrackID), a.id)
		} else {
			pool.Exec(ctx, `UPDATE managed_apps SET icon_url=$1, updated_at=now() WHERE id=$2`, iconURL, a.id)
		}
		log.Printf("backfill icon: %s → %s", a.bundleID, iconURL[:60]+"...")
	}
}

// depSchedulerRunner returns a port.DEPSchedulerRunner that is genuinely nil
// when the input *DEPScheduler is nil. Without this, Go boxes a typed-nil
// pointer into a non-nil interface value (interface{}(*X(nil)) != nil), and
// the controller's `if scheduler == nil` check would fail open, causing a
// panic on method call.
func depSchedulerRunner(s *service.DEPScheduler) port.DEPSchedulerRunner {
	if s == nil {
		return nil
	}
	return s
}
