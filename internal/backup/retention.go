package backup

import (
	"fmt"
	"sort"
	"time"
)

// Retention is how many backups to keep: the newest of each of the newest
// Daily days, Weekly ISO weeks and Monthly months (grandfather-father-son).
// The newest backup is always kept.
type Retention struct {
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
}

// DefaultRetention keeps a week of days, a month of weeks and half a year
// of months: at most 17 backups.
var DefaultRetention = Retention{Daily: 7, Weekly: 4, Monthly: 6}

// SelectExpired returns the backups of list that retention does not keep,
// counting days in loc. list may be in any order.
func SelectExpired(list []Info, keep Retention, loc *time.Location) []Info {
	sorted := make([]Info, len(list))
	copy(sorted, list)
	sortNewestFirst(sorted)

	kept := map[string]bool{}
	if len(sorted) > 0 {
		kept[sorted[0].Name] = true
	}
	mark := func(n int, period func(time.Time) string) {
		seen := map[string]bool{}
		for _, in := range sorted {
			if len(seen) >= n {
				return
			}
			p := period(in.Manifest.Created.In(loc))
			if seen[p] {
				continue
			}
			seen[p] = true
			kept[in.Name] = true
		}
	}
	mark(keep.Daily, func(t time.Time) string { return t.Format("2006-01-02") })
	mark(keep.Weekly, func(t time.Time) string {
		y, w := t.ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	})
	mark(keep.Monthly, func(t time.Time) string { return t.Format("2006-01") })

	var expired []Info
	for _, in := range sorted {
		if !kept[in.Name] {
			expired = append(expired, in)
		}
	}
	return expired
}

func sortNewestFirst(list []Info) {
	sort.SliceStable(list, func(i, j int) bool { return newer(list[i], list[j]) })
}

func newer(a, b Info) bool {
	if !a.Manifest.Created.Equal(b.Manifest.Created) {
		return a.Manifest.Created.After(b.Manifest.Created)
	}
	return a.Name > b.Name
}
