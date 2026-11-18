package backup

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.UTC)
	if err != nil {
		panic(err)
	}
	return t
}

func names(infos []Info) []string {
	out := make([]string, len(infos))
	for i, in := range infos {
		out[i] = in.Name
	}
	return out
}

func info(name, created string) Info {
	return Info{Name: name, Manifest: Manifest{Created: at(created)}}
}

// Keep the newest backup of each of the newest N days, weeks and months.
func TestSelectExpiredGrandfatherFatherSon(t *testing.T) {
	var list []Info
	// One backup a day from 2026-07-01 to 2026-11-13, newest first, plus a
	// second (manual) backup on the newest day.
	list = append(list, info("manual", "2026-11-13 12:00"))
	for d := at("2026-11-13 03:00"); !d.Before(at("2026-07-01 03:00")); d = d.AddDate(0, 0, -1) {
		list = append(list, info(d.Format("2006-01-02"), d.Format("2006-01-02 15:04")))
	}
	keep := Retention{Daily: 7, Weekly: 4, Monthly: 6}
	expired := SelectExpired(list, keep, time.UTC)

	kept := map[string]bool{}
	for _, in := range list {
		kept[in.Name] = true
	}
	for _, in := range expired {
		delete(kept, in.Name)
	}
	want := []string{
		// daily: the newest per day for 7 days (the manual one wins on 11-13)
		"manual", "2026-11-12", "2026-11-11", "2026-11-10", "2026-11-09", "2026-11-08", "2026-11-07",
		// weekly (ISO weeks, newest per week): 11-09..15 → manual, 11-02..08 → 11-08,
		// 10-26..11-01 → 11-01, 10-19..25 → 10-25
		"2026-11-01", "2026-10-25",
		// monthly: Nov → manual, Oct → 10-31, Sep → 09-30, Aug → 08-31, Jul → 07-31, (Jun none)
		"2026-10-31", "2026-09-30", "2026-08-31", "2026-07-31",
	}
	assert.ElementsMatch(t, want, keysOf(kept))
}

func keysOf(m map[string]bool) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The newest backup is always kept, whatever the counts.
func TestSelectExpiredAlwaysKeepsNewest(t *testing.T) {
	list := []Info{info("b", "2026-11-13 03:00"), info("a", "2026-11-12 03:00")}
	expired := SelectExpired(list, Retention{Daily: 0, Weekly: 0, Monthly: 0}, time.UTC)
	assert.Equal(t, []string{"a"}, names(expired))
}

// Days are counted in the device's time zone, not UTC.
func TestSelectExpiredUsesLocalDays(t *testing.T) {
	loc := time.FixedZone("UTC-3", -3*3600)
	// 01:00 UTC on the 13th is still the 12th at UTC-3.
	list := []Info{info("late", "2026-11-13 01:00"), info("evening", "2026-11-12 22:00")}
	expired := SelectExpired(list, Retention{Daily: 1}, loc)
	assert.Equal(t, []string{"evening"}, names(expired))
}
