package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newService(t *testing.T, in *install, c *clock) *Service {
	t.Helper()
	return NewService(ServiceOptions{
		Sources:  in.sources(),
		DBPath:   in.dbPath,
		Settings: in.store,
		Now:      c.Now,
		Location: time.UTC,
		Jitter:   func() time.Duration { return 7 * time.Minute },
	})
}

func TestSettingsDefaultsAndValidation(t *testing.T) {
	in := newInstall(t)
	s := newService(t, in, &clock{now: t0})

	got, err := s.Settings()
	require.NoError(t, err)
	assert.Equal(t, DefaultSettings(), got)
	assert.True(t, got.Enabled, "backups are on by default")
	assert.Equal(t, filepath.Join(in.dir, "backups"), s.DefaultDir())
	dir, err := s.Dir()
	require.NoError(t, err)
	assert.Equal(t, s.DefaultDir(), dir)

	usb := filepath.Join(t.TempDir(), "usb", "piworker")
	want := Settings{Enabled: true, Time: "22:05", Dir: usb, Retention: Retention{Daily: 3, Weekly: 2, Monthly: 1}}
	require.NoError(t, s.SaveSettings(want))
	got, err = s.Settings()
	require.NoError(t, err)
	assert.Equal(t, want, got)
	st, err := os.Stat(usb)
	require.NoError(t, err, "the directory is created and checked when saved")
	assert.Equal(t, os.FileMode(0o700), st.Mode().Perm())

	for name, bad := range map[string]Settings{
		"time":          {Enabled: true, Time: "25:00", Retention: DefaultRetention},
		"time format":   {Enabled: true, Time: "3am", Retention: DefaultRetention},
		"relative dir":  {Enabled: true, Time: "03:00", Dir: "backups", Retention: DefaultRetention},
		"no retention":  {Enabled: true, Time: "03:00", Retention: Retention{}},
		"negative":      {Enabled: true, Time: "03:00", Retention: Retention{Daily: -1, Weekly: 1}},
		"too many":      {Enabled: true, Time: "03:00", Retention: Retention{Daily: 1000}},
		"data dir":      {Enabled: true, Time: "03:00", Dir: in.dir, Retention: DefaultRetention},
		"not writeable": {Enabled: true, Time: "03:00", Dir: "/proc/piworker-backups", Retention: DefaultRetention},
	} {
		err := s.SaveSettings(bad)
		assert.ErrorIs(t, err, ErrInvalidSettings, name)
	}
}

func TestBackUpRecordsStatusPrunesAndSkipsUnchanged(t *testing.T) {
	in := newInstall(t)
	c := &clock{now: t0}
	s := newService(t, in, c)

	info, skipped, err := s.BackUp(context.Background(), ReasonScheduled)
	require.NoError(t, err)
	assert.False(t, skipped)
	st := s.Status()
	require.NotNil(t, st.LastSuccess)
	assert.True(t, st.LastSuccess.Equal(t0))
	assert.Equal(t, info.Name, st.LastName)
	assert.Empty(t, st.LastError)

	// Nothing changed a day later: the scheduled backup is skipped (no new
	// file), but the attempt counts as a success.
	c.now = t0.Add(24 * time.Hour)
	_, skipped, err = s.BackUp(context.Background(), ReasonScheduled)
	require.NoError(t, err)
	assert.True(t, skipped)
	list, err := s.List()
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.True(t, s.Status().LastSuccess.Equal(c.now))

	// A manual backup is never skipped.
	_, skipped, err = s.BackUp(context.Background(), ReasonManual)
	require.NoError(t, err)
	assert.False(t, skipped)

	// Retention runs after each backup: with Daily 1 only the newest day
	// stays.
	require.NoError(t, s.SaveSettings(Settings{Enabled: true, Time: "03:17", Retention: Retention{Daily: 1}}))
	mustExec(t, in.store.DB(), `UPDATE settings SET value = 'low' WHERE key = 'agent_deploy'`)
	c.now = t0.Add(48 * time.Hour)
	_, _, err = s.BackUp(context.Background(), ReasonScheduled)
	require.NoError(t, err)
	list, err = s.List()
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.True(t, list[0].Manifest.Created.Equal(c.now))
}

func TestBackUpFailureIsRecorded(t *testing.T) {
	in := newInstall(t)
	c := &clock{now: t0}
	s := newService(t, in, c)
	_, _, err := s.BackUp(context.Background(), ReasonManual)
	require.NoError(t, err)
	require.NoError(t, in.store.Close())
	c.now = t0.Add(time.Hour)
	_, _, err = s.BackUp(context.Background(), ReasonManual)
	require.Error(t, err)
	st := s.Status()
	assert.NotEmpty(t, st.LastError)
	assert.True(t, st.LastAttempt.Equal(c.now))
	assert.True(t, st.LastSuccess.Equal(t0), "the last success is kept")
}

func TestNextRun(t *testing.T) {
	in := newInstall(t)
	c := &clock{now: t0} // 03:17 UTC
	s := newService(t, in, c)

	// Never backed up: soon after start.
	assert.Equal(t, t0.Add(StartupDelay), s.NextRun())

	_, _, err := s.BackUp(context.Background(), ReasonScheduled)
	require.NoError(t, err)
	// Backed up now: tomorrow at 03:17 plus the jitter.
	assert.Equal(t, time.Date(2026, 11, 14, 3, 24, 0, 0, time.UTC), s.NextRun())

	// Same day, before the time: today.
	c.now = time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 11, 14, 3, 24, 0, 0, time.UTC), s.NextRun())

	// Missed for more than a day (the Pi was off): catch up soon.
	c.now = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, c.now.Add(StartupDelay), s.NextRun())

	// A failed attempt is retried an hour later, not in a loop.
	require.NoError(t, s.writeStatus(Status{LastAttempt: ptr(c.now), LastSuccess: ptr(t0), LastError: "disk full"}))
	assert.Equal(t, c.now.Add(time.Hour), s.NextRun())

	// Disabled: never.
	require.NoError(t, s.SaveSettings(Settings{Enabled: false, Time: "03:17", Retention: DefaultRetention}))
	assert.True(t, s.NextRun().IsZero())
}

func ptr(t time.Time) *time.Time { return &t }

func TestStageRestoreBacksUpFirstAndDoesNotPrune(t *testing.T) {
	in := newInstall(t)
	c := &clock{now: t0}
	s := newService(t, in, c)
	require.NoError(t, s.SaveSettings(Settings{Enabled: true, Time: "03:17", Retention: Retention{Daily: 1}}))
	first, _, err := s.BackUp(context.Background(), ReasonManual)
	require.NoError(t, err)
	mustExec(t, in.store.DB(), `UPDATE flows SET name = 'Changed'`)

	c.now = t0.Add(time.Hour)
	m, err := s.StageRestore(context.Background(), first.Name)
	require.NoError(t, err)
	assert.Equal(t, first.Manifest.ContentHash, m.ContentHash)
	assert.True(t, Pending(in.dbPath))

	list, err := s.List()
	require.NoError(t, err)
	require.Len(t, list, 2, "the restored backup stays although retention keeps one per day")
	assert.Equal(t, ReasonBeforeRestore, list[0].Manifest.Reason)

	_, err = s.StageRestore(context.Background(), "../piworker.db")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = s.StageRestore(context.Background(), "piworker-backup-20200101T000000Z.tar.gz")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDelete(t *testing.T) {
	in := newInstall(t)
	s := newService(t, in, &clock{now: t0})
	info, _, err := s.BackUp(context.Background(), ReasonManual)
	require.NoError(t, err)
	assert.ErrorIs(t, s.Delete("notes.txt"), ErrNotFound)
	require.NoError(t, s.Delete(info.Name))
	assert.ErrorIs(t, s.Delete(info.Name), ErrNotFound)
}

// Run backs up when due and stops with its context.
func TestRunBacksUpWhenDue(t *testing.T) {
	in := newInstall(t)
	s := NewService(ServiceOptions{
		Sources: in.sources(), DBPath: in.dbPath, Settings: in.store,
		Location: time.UTC, StartupDelay: 10 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	require.Eventually(t, func() bool { return s.Status().LastSuccess != nil }, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}
