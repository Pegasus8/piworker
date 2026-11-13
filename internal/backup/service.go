package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// SettingsKey is the settings-table key holding the backup settings.
const SettingsKey = "backup_settings"

// StatusFile, next to the database, records the last backup attempt. It is
// outside the database so that recording it does not change the content
// the next backup compares.
const StatusFile = "backup-status.json"

// StartupDelay is how long after start a backup that is due (never made,
// or missed for more than a day) runs.
const StartupDelay = 5 * time.Minute

// catchUpAfter: a scheduled backup missed for longer than this (the device
// was off at the scheduled time) runs soon after start.
const catchUpAfter = 26 * time.Hour

// retryAfter is the wait after a failed attempt.
const retryAfter = time.Hour

// maxJitter is the largest random delay after the scheduled time.
const maxJitter = 30 * time.Minute

// maxKeep bounds each retention count.
const maxKeep = 400

// Errors of the service.
var (
	ErrInvalidSettings = errors.New("invalid backup settings")
	ErrNotFound        = errors.New("backup not found")
)

// Settings are the admin's backup choices.
type Settings struct {
	Enabled bool `json:"enabled"`
	// Time is the local time of the daily backup, "HH:MM".
	Time string `json:"time"`
	// Dir is where backups are written; empty is the default (backups/
	// next to the database).
	Dir       string    `json:"dir"`
	Retention Retention `json:"retention"`
}

// DefaultSettings: daily at 03:17 into backups/ next to the database.
func DefaultSettings() Settings {
	return Settings{Enabled: true, Time: "03:17", Retention: DefaultRetention}
}

// Status is the outcome of the latest attempts.
type Status struct {
	LastAttempt   *time.Time `json:"lastAttempt,omitempty"`
	LastSuccess   *time.Time `json:"lastSuccess,omitempty"`
	LastScheduled *time.Time `json:"lastScheduled,omitempty"`
	LastError     string     `json:"lastError,omitempty"`
	// LastName and LastContentHash are the newest archive written.
	LastName        string `json:"lastName,omitempty"`
	LastContentHash string `json:"lastContentHash,omitempty"`
}

// SettingsStore persists the settings (the storage settings table).
type SettingsStore interface {
	GetSetting(key string) (string, bool, error)
	SetSetting(key, value string) error
}

// ServiceOptions configures a Service.
type ServiceOptions struct {
	Sources  Sources
	DBPath   string
	Settings SettingsStore
	Logger   zerolog.Logger
	// Now, Location, Jitter and StartupDelay default to the real clock,
	// time.Local, a random delay up to 30 minutes and StartupDelay.
	Now          func() time.Time
	Location     *time.Location
	Jitter       func() time.Duration
	StartupDelay time.Duration
}

// Service makes scheduled and manual backups, keeps the status, applies
// retention and stages restores. One operation runs at a time.
type Service struct {
	opts    ServiceOptions
	mu      sync.Mutex
	jitter  time.Duration
	changed chan struct{}
}

// NewService creates the service.
func NewService(opts ServiceOptions) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	if opts.Jitter == nil {
		opts.Jitter = func() time.Duration { return time.Duration(rand.Int64N(int64(maxJitter))) }
	}
	if opts.StartupDelay <= 0 {
		opts.StartupDelay = StartupDelay
	}
	return &Service{opts: opts, jitter: opts.Jitter(), changed: make(chan struct{}, 1)}
}

// DataDir is the directory of the database.
func (s *Service) DataDir() string { return filepath.Dir(s.opts.DBPath) }

// DefaultDir is backups/ next to the database.
func (s *Service) DefaultDir() string { return filepath.Join(s.DataDir(), "backups") }

// Settings returns the stored settings, or the defaults.
func (s *Service) Settings() (Settings, error) {
	raw, ok, err := s.opts.Settings.GetSetting(SettingsKey)
	if err != nil || !ok {
		return DefaultSettings(), err
	}
	set := DefaultSettings()
	if err := json.Unmarshal([]byte(raw), &set); err != nil {
		return DefaultSettings(), fmt.Errorf("read backup settings: %w", err)
	}
	return set, nil
}

var timeRe = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

// SaveSettings validates and stores the settings. The directory is created
// (0700) and must be writable.
func (s *Service) SaveSettings(set Settings) error {
	if !timeRe.MatchString(set.Time) {
		return fmt.Errorf("%w: the time must be HH:MM (24-hour)", ErrInvalidSettings)
	}
	r := set.Retention
	if r.Daily < 0 || r.Weekly < 0 || r.Monthly < 0 || r.Daily > maxKeep || r.Weekly > maxKeep || r.Monthly > maxKeep {
		return fmt.Errorf("%w: each retention count is between 0 and %d", ErrInvalidSettings, maxKeep)
	}
	if r.Daily+r.Weekly+r.Monthly == 0 {
		return fmt.Errorf("%w: keep at least one backup", ErrInvalidSettings)
	}
	if set.Dir != "" {
		if !filepath.IsAbs(set.Dir) {
			return fmt.Errorf("%w: the directory must be an absolute path", ErrInvalidSettings)
		}
		set.Dir = filepath.Clean(set.Dir)
		if set.Dir == filepath.Clean(s.DataDir()) {
			return fmt.Errorf("%w: use a directory of its own, not the data directory", ErrInvalidSettings)
		}
		if err := checkWritable(set.Dir); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidSettings, err)
		}
	}
	raw, err := json.Marshal(set)
	if err != nil {
		return err
	}
	if err := s.opts.Settings.SetSetting(SettingsKey, string(raw)); err != nil {
		return err
	}
	select {
	case s.changed <- struct{}{}:
	default:
	}
	return nil
}

// checkWritable creates dir (0700) if needed and writes a probe file.
func checkWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %v", dir, err)
	}
	f, err := os.CreateTemp(dir, ".probe-")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %v", dir, err)
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// Dir is the directory backups go to now.
func (s *Service) Dir() (string, error) {
	set, err := s.Settings()
	if err != nil {
		return "", err
	}
	if set.Dir == "" {
		return s.DefaultDir(), nil
	}
	return set.Dir, nil
}

// Status reads the status file; a missing or unreadable one is empty.
func (s *Service) Status() Status {
	var st Status
	raw, err := os.ReadFile(filepath.Join(s.DataDir(), StatusFile))
	if err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	return st
}

func (s *Service) writeStatus(st Status) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(s.DataDir(), StatusFile)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// List returns the backups in the current directory, newest first.
func (s *Service) List() ([]Info, error) {
	dir, err := s.Dir()
	if err != nil {
		return nil, err
	}
	return List(dir)
}

// BackUp makes a backup now. A scheduled backup whose content equals the
// newest archive (still present) is discarded and reported as skipped.
// Scheduled and manual backups apply retention afterwards.
func (s *Service) BackUp(ctx context.Context, reason string) (Info, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backUpLocked(ctx, reason)
}

func (s *Service) backUpLocked(ctx context.Context, reason string) (Info, bool, error) {
	now := s.opts.Now()
	st := s.Status()
	st.LastAttempt = &now
	fail := func(err error) (Info, bool, error) {
		st.LastError = err.Error()
		if werr := s.writeStatus(st); werr != nil {
			s.opts.Logger.Error().Err(werr).Msg("Cannot record the backup status")
		}
		s.opts.Logger.Error().Err(err).Str("reason", reason).Msg("Backup failed")
		return Info{}, false, err
	}
	set, err := s.Settings()
	if err != nil {
		return fail(err)
	}
	dir := set.Dir
	if dir == "" {
		dir = s.DefaultDir()
	}
	info, err := Create(ctx, s.opts.Sources, dir, reason, now)
	if err != nil {
		return fail(err)
	}
	skipped := false
	if reason == ReasonScheduled && st.LastContentHash == info.Manifest.ContentHash && st.LastName != "" &&
		exists(filepath.Join(dir, st.LastName)) {
		if err := os.Remove(filepath.Join(dir, info.Name)); err != nil {
			return fail(err)
		}
		skipped = true
	} else {
		st.LastName, st.LastContentHash = info.Name, info.Manifest.ContentHash
	}
	if reason != ReasonBeforeRestore {
		if err := s.prune(dir, set.Retention); err != nil {
			s.opts.Logger.Warn().Err(err).Msg("Cannot remove expired backups")
		}
	}
	st.LastSuccess = &now
	if reason == ReasonScheduled {
		st.LastScheduled = &now
		s.jitter = s.opts.Jitter()
	}
	st.LastError = ""
	if err := s.writeStatus(st); err != nil {
		s.opts.Logger.Error().Err(err).Msg("Cannot record the backup status")
	}
	ev := s.opts.Logger.Info().Str("reason", reason)
	if skipped {
		ev.Msg("Scheduled backup skipped: nothing changed since the last one")
	} else {
		ev.Str("file", info.Name).Int64("bytes", info.Size).Msg("Backup created")
	}
	return info, skipped, nil
}

func (s *Service) prune(dir string, keep Retention) error {
	list, err := List(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, in := range SelectExpired(list, keep, s.opts.Location) {
		if err := os.Remove(filepath.Join(dir, in.Name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		} else {
			s.opts.Logger.Info().Str("file", in.Name).Msg("Expired backup removed")
		}
	}
	return errors.Join(errs...)
}

// path resolves a backup name in the current directory.
func (s *Service) path(name string) (string, error) {
	if !IsBackupName(name) {
		return "", ErrNotFound
	}
	dir, err := s.Dir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return p, nil
}

// RestorePending reports whether a restore is staged for the next start.
func (s *Service) RestorePending() bool { return Pending(s.opts.DBPath) }

// Delete removes a backup by name.
func (s *Service) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// StageRestore validates and stages the named backup, then backs up the
// current state (without retention, so the backup being restored stays).
// The restore is applied at the next start.
func (s *Service) StageRestore(ctx context.Context, name string) (Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(name)
	if err != nil {
		return Manifest{}, err
	}
	m, err := Stage(p, s.opts.DBPath)
	if err != nil {
		return Manifest{}, err
	}
	if _, _, err := s.backUpLocked(ctx, ReasonBeforeRestore); err != nil {
		if cerr := CancelPending(s.opts.DBPath); cerr != nil {
			s.opts.Logger.Error().Err(cerr).Msg("Cannot cancel the staged restore")
		}
		return Manifest{}, fmt.Errorf("back up the current state first: %w", err)
	}
	s.opts.Logger.Warn().Str("file", name).Time("created", m.Created).Msg("Restore staged; it is applied when PiWorker starts again")
	return m, nil
}

// NextRun is when the next scheduled backup runs; zero when disabled.
func (s *Service) NextRun() time.Time {
	set, err := s.Settings()
	if err != nil || !set.Enabled {
		return time.Time{}
	}
	now := s.opts.Now()
	st := s.Status()
	if st.LastError != "" && st.LastAttempt != nil {
		retry := st.LastAttempt.Add(retryAfter)
		if retry.Before(now) {
			return now
		}
		return retry
	}
	if st.LastSuccess == nil || now.Sub(*st.LastSuccess) > catchUpAfter {
		return now.Add(s.opts.StartupDelay)
	}
	hh, _ := strconv.Atoi(set.Time[:2])
	mm, _ := strconv.Atoi(set.Time[3:])
	local := now.In(s.opts.Location)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.opts.Location)
	for i := 0; i < 3; i++ {
		d := day.AddDate(0, 0, i)
		at := time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, s.opts.Location).Add(s.jitter)
		if !at.After(now) {
			continue
		}
		if st.LastScheduled != nil && sameDay(st.LastScheduled.In(s.opts.Location), d) {
			continue
		}
		return at
	}
	return now.Add(24 * time.Hour)
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// maxWait re-evaluates the schedule at least this often (clock changes,
// a suspended device).
const maxWait = 10 * time.Minute

// Run makes the scheduled backups until ctx ends.
func (s *Service) Run(ctx context.Context) {
	for {
		next := s.NextRun()
		wait := maxWait
		if !next.IsZero() {
			if d := next.Sub(s.opts.Now()); d < wait {
				wait = max(d, 0)
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.changed:
			timer.Stop()
			continue
		case <-timer.C:
		}
		if next.IsZero() || s.opts.Now().Before(next) {
			continue
		}
		_, _, _ = s.BackUp(ctx, ReasonScheduled)
	}
}
