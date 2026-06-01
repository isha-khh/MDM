package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/anthropics/mdm-server/internal/domain"
)

// ── fakes ─────────────────────────────────────────────────────────────

type fakeABM struct {
	devices []domain.ABMDevice
	err     error
}

func (f *fakeABM) ListOrgDevices(_ context.Context) ([]domain.ABMDevice, error) {
	return f.devices, f.err
}

type fakeMDM struct {
	mu        sync.Mutex
	calls     []fakeMDMCall
	returnErr error
}

type fakeMDMCall struct {
	template map[string]interface{}
	serials  []string
}

func (f *fakeMDM) DefineDEPProfile(_ context.Context, tpl map[string]interface{}, serials []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.returnErr != nil {
		return "", f.returnErr
	}
	f.calls = append(f.calls, fakeMDMCall{template: tpl, serials: append([]string(nil), serials...)})
	return fmt.Sprintf("uuid-%d", len(f.calls)), nil
}

// Unused MicroMDMClient methods — only DefineDEPProfile is called by scheduler.
func (f *fakeMDM) ListDevices(context.Context) ([]*domain.Device, error)          { panic("unused") }
func (f *fakeMDM) GetDevice(context.Context, string) (*domain.Device, error)      { panic("unused") }
func (f *fakeMDM) SendCommand(context.Context, map[string]interface{}) (*domain.CommandResult, error) {
	panic("unused")
}
func (f *fakeMDM) SendPush(context.Context, string) error                          { panic("unused") }
func (f *fakeMDM) ClearQueue(context.Context, string) (*domain.CommandResult, error) { panic("unused") }
func (f *fakeMDM) InspectQueue(context.Context, string) (string, error)           { panic("unused") }
func (f *fakeMDM) SyncDEP(context.Context) error                                  { panic("unused") }

type fakeRepo struct {
	mu     sync.Mutex
	stored map[string]*domain.DEPAssignment
}

func newFakeRepo() *fakeRepo { return &fakeRepo{stored: map[string]*domain.DEPAssignment{}} }

func (r *fakeRepo) Get(_ context.Context, serial string) (*domain.DEPAssignment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stored[serial], nil
}

func (r *fakeRepo) Upsert(_ context.Context, a *domain.DEPAssignment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stored[a.SerialNumber] = a
	return nil
}

func (r *fakeRepo) ListSerials(_ context.Context) (map[string]bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]bool, len(r.stored))
	for k := range r.stored {
		out[k] = true
	}
	return out, nil
}

// ── helpers ───────────────────────────────────────────────────────────

func writeTemplate(t *testing.T, dir, family string, content map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(content)
	if err := os.WriteFile(filepath.Join(dir, family+".json"), b, 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
}

// ── tests ─────────────────────────────────────────────────────────────

func TestFamilyToTemplate(t *testing.T) {
	cases := map[string]string{
		"Mac":      "mac",
		"mac":      "mac",
		"iPad":     "ipad",
		"IPAD":     "ipad",
		"iPhone":   "iphone",
		"AppleTV":  "appletv",
		"Watch":    "", // not supported
		"":         "",
	}
	for input, want := range cases {
		if got := familyToTemplate(input); got != want {
			t.Errorf("familyToTemplate(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestScheduler_AppliesNewDevicesByFamily(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "mac", map[string]interface{}{"profile_name": "Mac DEP", "url": "https://mdm.example/enroll"})
	writeTemplate(t, dir, "ipad", map[string]interface{}{"profile_name": "iPad DEP", "url": "https://mdm.example/enroll"})

	abm := &fakeABM{devices: []domain.ABMDevice{
		{Serial: "MAC001", ProductFamily: "Mac"},
		{Serial: "MAC002", ProductFamily: "Mac"},
		{Serial: "IPAD001", ProductFamily: "iPad"},
		{Serial: "WATCH001", ProductFamily: "Watch"}, // unsupported, should skip
	}}
	mdm := &fakeMDM{}
	repo := newFakeRepo()

	s := NewDEPScheduler(abm, mdm, repo, dir, 0)
	s.stepDelay = 0 // no sleep in tests
	s.RunOnce(context.Background())

	// 3 supported devices should have been applied.
	if got := len(mdm.calls); got != 3 {
		t.Fatalf("expected 3 PUT calls, got %d", got)
	}
	if got := len(repo.stored); got != 3 {
		t.Fatalf("expected 3 stored rows, got %d", got)
	}

	// Check template routing by family.
	for _, c := range mdm.calls {
		if len(c.serials) != 1 {
			t.Errorf("expected 1 serial per call, got %d", len(c.serials))
			continue
		}
		serial := c.serials[0]
		name, _ := c.template["profile_name"].(string)
		switch serial {
		case "MAC001", "MAC002":
			if name != "Mac DEP" {
				t.Errorf("serial=%s got template profile_name=%q, want Mac DEP", serial, name)
			}
		case "IPAD001":
			if name != "iPad DEP" {
				t.Errorf("serial=%s got template profile_name=%q, want iPad DEP", serial, name)
			}
		default:
			t.Errorf("unexpected serial in call: %s", serial)
		}
	}
}

func TestScheduler_SkipsKnownDevices(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "mac", map[string]interface{}{"profile_name": "Mac DEP"})

	abm := &fakeABM{devices: []domain.ABMDevice{
		{Serial: "MAC001", ProductFamily: "Mac"},
		{Serial: "MAC002", ProductFamily: "Mac"},
	}}
	mdm := &fakeMDM{}
	repo := newFakeRepo()
	// Pre-seed MAC001 — it should NOT be re-applied.
	_ = repo.Upsert(context.Background(), &domain.DEPAssignment{
		SerialNumber: "MAC001", ProductFamily: "Mac", TemplateFamily: "mac", ProfileUUID: "pre-existing",
	})

	s := NewDEPScheduler(abm, mdm, repo, dir, 0)
	s.stepDelay = 0
	s.RunOnce(context.Background())

	if got := len(mdm.calls); got != 1 {
		t.Fatalf("expected 1 PUT call (MAC002 only), got %d", got)
	}
	if mdm.calls[0].serials[0] != "MAC002" {
		t.Errorf("expected MAC002 to be applied, got %s", mdm.calls[0].serials[0])
	}
}

func TestScheduler_SkipsMissingTemplate(t *testing.T) {
	dir := t.TempDir()
	// No template files written — every device should be skipped, no rows written.

	abm := &fakeABM{devices: []domain.ABMDevice{{Serial: "MAC001", ProductFamily: "Mac"}}}
	mdm := &fakeMDM{}
	repo := newFakeRepo()

	s := NewDEPScheduler(abm, mdm, repo, dir, 0)
	s.stepDelay = 0
	s.RunOnce(context.Background())

	if len(mdm.calls) != 0 {
		t.Errorf("expected 0 PUT calls (template missing), got %d", len(mdm.calls))
	}
	if len(repo.stored) != 0 {
		t.Errorf("expected 0 stored rows (skip without writing), got %d", len(repo.stored))
	}
}

func TestScheduler_MDMErrorLeavesRowUnwritten(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, dir, "mac", map[string]interface{}{"profile_name": "Mac DEP"})

	abm := &fakeABM{devices: []domain.ABMDevice{{Serial: "MAC001", ProductFamily: "Mac"}}}
	mdm := &fakeMDM{returnErr: errors.New("micromdm: 500")}
	repo := newFakeRepo()

	s := NewDEPScheduler(abm, mdm, repo, dir, 0)
	s.stepDelay = 0
	s.RunOnce(context.Background())

	// No row stored, so the next cycle will retry — that's the desired behaviour.
	if len(repo.stored) != 0 {
		t.Errorf("expected 0 stored rows on MDM error, got %d", len(repo.stored))
	}
}
