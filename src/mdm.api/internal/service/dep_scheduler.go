package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/port"
)

// DEPScheduler polls Apple Business Manager every PollInterval, finds devices
// that don't yet have a DEP profile applied (per the dep_assignments table),
// loads the matching template from disk by productFamily, and asks MicroMDM to
// apply it. New rows are written to dep_assignments on success.
//
// Design notes:
//   - We rely on ABM's default platform assignment to route new devices to
//     our MDM server. So we only READ from ABM (no orgDeviceActivities).
//   - One PUT /v1/dep/profiles per new serial. Apple returns a fresh
//     profile_uuid each time; older assignments on other serials are not
//     touched. A 200ms sleep between calls is conservative — Apple's DEP
//     rate limit is generous.
//   - Template files (mac.json, ipad.json, iphone.json) live in templateDir.
//     Missing templates skip the device with a warning, not a DB row, so
//     they retry every cycle until the template appears.
type DEPScheduler struct {
	abm          port.ABMClient
	mdm          port.MicroMDMClient
	repo         port.DEPAssignmentRepo
	templateDir  string
	pollInterval time.Duration

	// throttle between PUTs, configurable for tests.
	stepDelay time.Duration

	// running guards against overlapping RunOnce calls. The startup tick and
	// the manual "立即套用" button can otherwise both fire at once, doubling
	// every Apple DEP PUT (each call mints a new profile UUID, wasting calls).
	running atomic.Bool
}

func NewDEPScheduler(abm port.ABMClient, mdm port.MicroMDMClient, repo port.DEPAssignmentRepo, templateDir string, pollInterval time.Duration) *DEPScheduler {
	return &DEPScheduler{
		abm:          abm,
		mdm:          mdm,
		repo:         repo,
		templateDir:  templateDir,
		pollInterval: pollInterval,
		stepDelay:    200 * time.Millisecond,
	}
}

// Start launches the scheduler loop. It runs once immediately on startup
// then on pollInterval. Stops cleanly when ctx is cancelled.
func (s *DEPScheduler) Start(ctx context.Context) {
	go s.loop(ctx)
}

func (s *DEPScheduler) loop(ctx context.Context) {
	log.Printf("[dep-scheduler] starting, interval=%s template_dir=%s", s.pollInterval, s.templateDir)
	s.RunOnce(ctx)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("[dep-scheduler] stopping")
			return
		case <-ticker.C:
			s.RunOnce(ctx)
		}
	}
}

// RunOnce performs one poll cycle. Exposed for unit tests and manual triggers.
// Concurrent calls are serialised — the second caller logs + returns immediately
// rather than waiting (callers don't usually want to block; the in-flight cycle
// will reach their device on its next iteration anyway).
func (s *DEPScheduler) RunOnce(ctx context.Context) {
	// Defensive against typed-nil interface call. The HTTP layer's nil check
	// is the primary gate, but this catches misconfiguration paths.
	if s == nil {
		log.Println("[dep-scheduler] RunOnce called on nil scheduler — ignored")
		return
	}
	if !s.running.CompareAndSwap(false, true) {
		log.Println("[dep-scheduler] another cycle is already running — skipping this trigger")
		return
	}
	defer s.running.Store(false)

	devices, err := s.abm.ListOrgDevices(ctx)
	if err != nil {
		log.Printf("[dep-scheduler] list ABM devices: %v", err)
		return
	}
	known, err := s.repo.ListSerials(ctx)
	if err != nil {
		log.Printf("[dep-scheduler] list known assignments: %v", err)
		return
	}

	newCount, skipCount, errCount := 0, 0, 0
	for _, d := range devices {
		if d.Serial == "" || known[d.Serial] {
			continue
		}
		result := s.applyOne(ctx, d)
		switch result {
		case applyApplied:
			newCount++
		case applySkipped:
			skipCount++
		case applyError:
			errCount++
		}
		if s.stepDelay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.stepDelay):
			}
		}
	}
	if newCount > 0 || errCount > 0 {
		log.Printf("[dep-scheduler] cycle done: applied=%d skipped=%d errors=%d (ABM total=%d, known=%d)",
			newCount, skipCount, errCount, len(devices), len(known))
	}
}

type applyResult int

const (
	applyApplied applyResult = iota
	applySkipped
	applyError
)

func (s *DEPScheduler) applyOne(ctx context.Context, d domain.ABMDevice) applyResult {
	family := familyToTemplate(d.ProductFamily)
	if family == "" {
		log.Printf("[dep-scheduler] skip %s: unknown productFamily %q", d.Serial, d.ProductFamily)
		return applySkipped
	}

	template, err := s.loadTemplate(family)
	if err != nil {
		// Template missing or unparseable — treat as transient: don't write a row,
		// retry next cycle once user drops in the file.
		log.Printf("[dep-scheduler] skip %s (family=%s): %v", d.Serial, family, err)
		return applySkipped
	}

	profileUUID, err := s.mdm.DefineDEPProfile(ctx, template, []string{d.Serial})
	if err != nil {
		log.Printf("[dep-scheduler] apply failed serial=%s family=%s: %v", d.Serial, family, err)
		// Record the failure so an operator can see what's wrong, but mark with
		// empty profile_uuid so a future retry can pick it up. We intentionally
		// do NOT write a row on transient errors — otherwise we'd never retry.
		return applyError
	}

	err = s.repo.Upsert(ctx, &domain.DEPAssignment{
		SerialNumber:   d.Serial,
		ProductFamily:  d.ProductFamily,
		TemplateFamily: family,
		ProfileUUID:    profileUUID,
		AppliedAt:      time.Now(),
	})
	if err != nil {
		log.Printf("[dep-scheduler] db upsert failed serial=%s: %v", d.Serial, err)
		return applyError
	}
	log.Printf("[dep-scheduler] applied serial=%s family=%s profile_uuid=%s", d.Serial, family, profileUUID)
	return applyApplied
}

// familyToTemplate normalises ABM productFamily to a lowercase template basename.
// Returns "" for unknown families so the scheduler skips them.
func familyToTemplate(productFamily string) string {
	switch strings.ToLower(productFamily) {
	case "mac":
		return "mac"
	case "ipad":
		return "ipad"
	case "iphone":
		return "iphone"
	case "appletv":
		return "appletv"
	default:
		return ""
	}
}

func (s *DEPScheduler) loadTemplate(family string) (map[string]interface{}, error) {
	path := filepath.Join(s.templateDir, family+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("template not found at %s: %w", path, err)
	}
	var tpl map[string]interface{}
	if err := json.Unmarshal(data, &tpl); err != nil {
		return nil, fmt.Errorf("parse template %s: %w", path, err)
	}
	return tpl, nil
}
