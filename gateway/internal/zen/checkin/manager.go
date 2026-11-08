// Package checkin schedules confirmed daily rewards without storing credentials.
package checkin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var BusinessZone = time.FixedZone("Asia/Shanghai", 8*60*60)
var ErrBusy = errors.New("签到正在执行，请等待本轮完成")

type Outcome struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	// CoveredDay is set only when the provider confirms this business day.
	CoveredDay string   `json:"coveredDay,omitempty"`
	Amount     *float64 `json:"amount,omitempty"`
}

type Target struct {
	Channel string `json:"channel"`
	Key     string `json:"key"` // opaque digest; never a token or upstream user ID
	Label   string `json:"label"`
	Mode    string `json:"mode"` // automatic, manual, unsupported
	Reason  string `json:"reason,omitempty"`
}

func AccountKey(channel, identity string) string {
	h := sha256.Sum256([]byte(channel + "\x00" + identity))
	return hex.EncodeToString(h[:16])
}

type Settings struct {
	Enabled  bool            `json:"enabled"`
	Times    []string        `json:"times"`
	Disabled map[string]bool `json:"disabled"`
}

type Record struct {
	Outcome
	Day           string `json:"day"`
	Slot          string `json:"slot"`
	Attempts      int    `json:"attempts"`
	UpdatedAt     int64  `json:"updatedAt"`
	NextAttemptAt int64  `json:"nextAttemptAt,omitempty"`
}

type diskState struct {
	Version  int               `json:"version"`
	Settings Settings          `json:"settings"`
	Records  map[string]Record `json:"records"`
}

type Row struct {
	Target
	Enabled bool   `json:"enabled"`
	Result  Record `json:"result"`
}

type View struct {
	Settings Settings `json:"settings"`
	Timezone string   `json:"timezone"`
	Today    string   `json:"today"`
	Running  bool     `json:"running"`
	Error    string   `json:"error,omitempty"`
	Accounts []Row    `json:"accounts"`
}

type Manager struct {
	mu           sync.Mutex
	path         string
	state        diskState
	running      bool
	storageError string
	list         func() []Target
	claim        func(context.Context, Target, string) Outcome
	now          func() time.Time
}

func New(home string, list func() []Target, claim func(context.Context, Target, string) Outcome) *Manager {
	m := &Manager{path: filepath.Join(home, "checkin.json"), list: list, claim: claim, now: time.Now,
		state: diskState{Version: 1, Settings: Settings{Enabled: true, Times: []string{"09:00", "21:00"}, Disabled: map[string]bool{}}, Records: map[string]Record{}}}
	b, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return m
	}
	if err != nil {
		m.storageError = "无法读取签到记录，请检查数据目录权限"
		return m
	}
	var state diskState
	if json.Unmarshal(b, &state) != nil || state.Version != 1 || state.Records == nil || validateSettings(&state.Settings) != nil {
		m.storageError = "签到记录损坏或版本不兼容；已停止领取，请先备份并修复 checkin.json"
		return m
	}
	m.state = state
	return m
}

func validateSettings(s *Settings) error {
	if len(s.Times) == 0 || len(s.Times) > 4 {
		return errors.New("每天设置 1 至 4 个签到时间")
	}
	seen := map[string]bool{}
	for _, value := range s.Times {
		t, err := time.Parse("15:04", value)
		if err != nil || t.Format("15:04") != value || seen[value] {
			return errors.New("时间请使用不重复的 HH:MM 格式")
		}
		seen[value] = true
	}
	sort.Strings(s.Times)
	if s.Disabled == nil {
		s.Disabled = map[string]bool{}
	}
	return nil
}

func (m *Manager) saveLocked() error {
	b, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), ".checkin-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, m.path)
	}
	return err
}

func (m *Manager) Update(settings Settings) error {
	if err := validateSettings(&settings); err != nil {
		return err
	}
	// Clone user-owned collections before retaining them.
	b, _ := json.Marshal(settings)
	var next Settings
	_ = json.Unmarshal(b, &next)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.storageError != "" {
		return errors.New(m.storageError)
	}
	previous := m.state.Settings
	m.state.Settings = next
	if err := m.saveLocked(); err != nil {
		m.state.Settings = previous
		return errors.New("签到设置保存失败，请检查数据目录权限")
	}
	return nil
}

func (m *Manager) Snapshot() View {
	targets := m.list()
	m.mu.Lock()
	defer m.mu.Unlock()
	b, _ := json.Marshal(m.state.Settings)
	var settings Settings
	_ = json.Unmarshal(b, &settings)
	v := View{Settings: settings, Timezone: "Asia/Shanghai (UTC+8)", Today: m.now().In(BusinessZone).Format(time.DateOnly), Running: m.running, Error: m.storageError, Accounts: []Row{}}
	for _, target := range targets {
		v.Accounts = append(v.Accounts, Row{Target: target, Enabled: !settings.Disabled[target.Channel], Result: m.state.Records[target.Key]})
	}
	return v
}

// Start has a cancellable startup delay; it never launches browser or model calls.
func (m *Manager) Start(ctx context.Context) {
	t := time.NewTimer(30 * time.Second)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return
	case <-t.C:
	}
	_ = m.Run(ctx, false)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.Run(ctx, false)
		}
	}
}

func currentSlot(now time.Time, times []string) string {
	local := now.In(BusinessZone)
	latest := ""
	for _, at := range times {
		if local.Format("15:04") >= at {
			latest = at
		}
	}
	if latest == "" {
		return ""
	}
	return local.Format(time.DateOnly) + "T" + latest
}

func (m *Manager) begin() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.storageError != "" {
		return errors.New(m.storageError)
	}
	if m.running {
		return ErrBusy
	}
	m.running = true
	return nil
}

// Trigger reserves the run before returning so repeated clicks cannot queue batches.
func (m *Manager) Trigger(ctx context.Context) error {
	if err := m.begin(); err != nil {
		return err
	}
	go m.run(ctx, true)
	return nil
}

func (m *Manager) Run(ctx context.Context, manual bool) error {
	if err := m.begin(); err != nil {
		return err
	}
	return m.run(ctx, manual)
}

func (m *Manager) run(ctx context.Context, manual bool) error {
	defer func() { m.mu.Lock(); m.running = false; m.mu.Unlock() }()
	targets := m.list()
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if target.Mode != "automatic" {
			continue
		}
		m.mu.Lock()
		now := m.now()
		day := now.In(BusinessZone).Format(time.DateOnly)
		slot := currentSlot(now, m.state.Settings.Times)
		old := m.state.Records[target.Key]
		if m.state.Settings.Disabled[target.Channel] || (!manual && (!m.state.Settings.Enabled || slot == "")) || old.CoveredDay == day {
			m.mu.Unlock()
			continue
		}
		if slot == "" {
			slot = day + "Tmanual"
		}
		if !manual && old.Day == day {
			// Authentication failures wait for an explicit retry or tomorrow. Inactive
			// campaigns are rechecked in the next configured slot, not every minute.
			if old.Status == "login_required" || old.Status == "manual" || (old.Slot == slot && (old.Status == "inactive" || old.Attempts >= 4 || old.NextAttemptAt > now.UnixMilli())) {
				m.mu.Unlock()
				continue
			}
		}
		attempts := 1
		if old.Day == day && old.Slot == slot {
			attempts = old.Attempts + 1
		}
		// Persist a reservation BEFORE the write request. A crash cannot spin a
		// claim loop on restart; upstream idempotency resolves an uncertain result.
		record := Record{Outcome: Outcome{Status: "pending", Message: "正在领取；中断后将核实结果"}, Day: day, Slot: slot, Attempts: attempts, UpdatedAt: now.UnixMilli(), NextAttemptAt: now.Add(5 * time.Minute).UnixMilli()}
		m.state.Records[target.Key] = record
		if err := m.saveLocked(); err != nil {
			m.state.Records[target.Key] = old
			m.storageError = "签到记录保存失败，已停止领取，请检查数据目录权限"
			m.mu.Unlock()
			return errors.New("签到记录保存失败，未发送领取请求")
		}
		m.mu.Unlock()
		callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		outcome := m.claim(callCtx, target, day)
		cancel()
		if outcome.Status == "claimed" || outcome.Status == "already" {
			if outcome.CoveredDay != day {
				outcome.CoveredDay = ""
				outcome.Message += "；尚未确认覆盖今日"
			}
		} else {
			outcome.CoveredDay = ""
		}
		record.Outcome = outcome
		record.UpdatedAt = m.now().UnixMilli()
		record.NextAttemptAt = 0
		if outcome.CoveredDay != day && outcome.Status != "login_required" && outcome.Status != "manual" && outcome.Status != "inactive" && attempts < 4 {
			delays := []time.Duration{5 * time.Minute, 15 * time.Minute, time.Hour, 3 * time.Hour}
			record.NextAttemptAt = m.now().Add(delays[min(attempts-1, len(delays)-1)]).UnixMilli()
		}
		m.mu.Lock()
		m.state.Records[target.Key] = record
		// Keep at most two business days and no credentials or account labels.
		cutoff := m.now().In(BusinessZone).AddDate(0, 0, -1).Format(time.DateOnly)
		for key, item := range m.state.Records {
			if item.Day < cutoff {
				delete(m.state.Records, key)
			}
		}
		err := m.saveLocked()
		if err != nil {
			m.storageError = "领取结果保存失败；已停止后续签到，请检查数据目录权限"
		}
		m.mu.Unlock()
		if err != nil {
			return errors.New("领取结果保存失败，已停止后续签到")
		}
	}
	return nil
}
