package builtin

import (
	"context"
	"fmt"
	"math"
	"runtime/debug"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog/log"
)

// sunZenith is the sun's zenith angle at each event: sunrise and sunset
// (with refraction and the disc's radius) and civil dawn and dusk.
var sunZenith = map[string]float64{"sunrise": 90.833, "sunset": 90.833, "dawn": 96, "dusk": 96}

// sunEventOn returns when event happens on the UTC day of day at latitude
// and longitude (degrees, east positive), with the NOAA sunrise algorithm
// (within a couple of minutes). ok is false when it does not happen that day
// (polar day or night).
func sunEventOn(day time.Time, lat, lon float64, event string) (time.Time, bool) {
	zenith, known := sunZenith[event]
	if !known {
		return time.Time{}, false
	}
	rising := event == "sunrise" || event == "dawn"
	rad := math.Pi / 180
	n := float64(day.YearDay())
	lngHour := lon / 15
	t := n + (18-lngHour)/24
	if rising {
		t = n + (6-lngHour)/24
	}
	m := 0.9856*t - 3.289
	l := math.Mod(m+1.916*math.Sin(m*rad)+0.020*math.Sin(2*m*rad)+282.634+360, 360)
	ra := math.Mod(math.Atan(0.91764*math.Tan(l*rad))/rad+360, 360)
	ra += math.Floor(l/90)*90 - math.Floor(ra/90)*90
	ra /= 15
	sinDec := 0.39782 * math.Sin(l*rad)
	cosDec := math.Cos(math.Asin(sinDec))
	cosH := (math.Cos(zenith*rad) - sinDec*math.Sin(lat*rad)) / (cosDec * math.Cos(lat*rad))
	if cosH > 1 || cosH < -1 {
		return time.Time{}, false
	}
	h := math.Acos(cosH) / rad
	if rising {
		h = 360 - h
	}
	h /= 15
	ut := math.Mod(h+ra-0.06571*t-6.622-lngHour+48, 24)
	midnight := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	return midnight.Add(time.Duration(ut * float64(time.Hour))), true
}

// nextSunEvent is the first time after now that event (moved by offset)
// happens, searching about a year ahead.
func nextSunEvent(now time.Time, lat, lon float64, event string, offset time.Duration) (time.Time, bool) {
	day := now.UTC().AddDate(0, 0, -1)
	for i := 0; i < 370; i++ {
		if at, ok := sunEventOn(day.AddDate(0, 0, i), lat, lon, event); ok {
			if at = at.Add(offset); at.After(now) {
				return at, true
			}
		}
	}
	return time.Time{}, false
}

// SunTrigger fires at sunrise, sunset, dawn or dusk (± an offset) at a
// place, computed locally (no network).
type SunTrigger struct {
	*node.BaseNode

	latitude, longitude float64
	event               string
	offset              time.Duration
	next                func(now time.Time) (time.Time, bool) // nextSunEvent; tests replace it

	run triggerRun
}

// NewSunTrigger creates a SunTrigger from configuration.
func NewSunTrigger(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	if _, ok := config["latitude"]; !ok {
		return nil, fmt.Errorf("latitude is required (degrees, south negative)")
	}
	if _, ok := config["longitude"]; !ok {
		return nil, fmt.Errorf("longitude is required (degrees, west negative)")
	}
	lat, lon := base.GetConfigFloat("latitude", 0), base.GetConfigFloat("longitude", 0)
	if math.IsNaN(lat) || lat < -90 || lat > 90 {
		return nil, fmt.Errorf("latitude must be between -90 and 90 degrees")
	}
	if math.IsNaN(lon) || lon < -180 || lon > 180 {
		return nil, fmt.Errorf("longitude must be between -180 and 180 degrees")
	}
	event := base.GetConfigString("event", "sunset")
	if _, ok := sunZenith[event]; !ok {
		return nil, fmt.Errorf("event must be sunrise, sunset, dawn or dusk")
	}
	offset := base.GetConfigFloat("offset", 0)
	if math.IsNaN(offset) || offset < -720 || offset > 720 {
		return nil, fmt.Errorf("offset must be between -720 and 720 minutes")
	}
	t := &SunTrigger{BaseNode: base, latitude: lat, longitude: lon, event: event,
		offset: time.Duration(offset * float64(time.Minute))}
	t.next = func(now time.Time) (time.Time, bool) {
		return nextSunEvent(now, t.latitude, t.longitude, t.event, t.offset)
	}
	return t, nil
}

// Process is unused for trigger nodes.
func (t *SunTrigger) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}

var sunOutputs = []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}}

// Ports returns the port definitions.
func (t *SunTrigger) Ports() (inputs []types.Port, outputs []types.Port) { return nil, sunOutputs }

// Validate checks the configuration (the constructor already did).
func (t *SunTrigger) Validate() error { return nil }

// Start waits for each event in turn.
func (t *SunTrigger) Start(ctx context.Context, out chan<- *types.Message) error {
	stopCh, done, err := t.run.begin()
	if err != nil {
		return err
	}
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Bytes("stack", debug.Stack()).Msg("sun trigger goroutine panicked")
			}
		}()
		for {
			at, ok := t.next(time.Now())
			if !ok {
				log.Warn().Str("event", t.event).Msg("sun trigger: the event does not happen within a year here")
				return
			}
			timer := time.NewTimer(time.Until(at))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-stopCh:
				timer.Stop()
				return
			case <-timer.C:
			}
			msg := types.NewMessage(map[string]interface{}{"event": t.event, "time": at.UTC().Format(time.RFC3339),
				"latitude": t.latitude, "longitude": t.longitude}, types.DataTypeObject)
			msg.SourcePort = "output"
			msg.Topic = "sun"
			select {
			case out <- msg:
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			default:
				metrics.IncrementMessagesDropped()
				log.Warn().Msg("sun trigger message dropped: channel full")
			}
		}
	}()
	return nil
}

// Stop stops waiting.
func (t *SunTrigger) Stop() error {
	t.run.end()
	return nil
}

func sunConfigSchema() node.ConfigSchema {
	minLat, maxLat, minLon, maxLon, minOff, maxOff := -90.0, 90.0, -180.0, 180.0, -720.0, 720.0
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"latitude": {Type: "number", Title: "Latitude", Description: "Of the place, in degrees (south negative), e.g. -34.6",
				Minimum: &minLat, Maximum: &maxLat, Unit: "degrees", AgentVisible: true},
			"longitude": {Type: "number", Title: "Longitude", Description: "Of the place, in degrees (west negative), e.g. -58.4",
				Minimum: &minLon, Maximum: &maxLon, Unit: "degrees", AgentVisible: true},
			"event": {Type: "string", Title: "Event", Description: "sunrise, sunset, or civil dawn / dusk (sun 6° below the horizon)",
				Default: "sunset", Enum: []string{"sunrise", "sunset", "dawn", "dusk"}, AgentVisible: true},
			"offset": {Type: "number", Title: "Offset (min)", Description: "Shift from the event, in minutes: negative before, positive after",
				Default: 0, Minimum: &minOff, Maximum: &maxOff, Unit: "minutes", AgentVisible: true},
		},
		Required: []string{"latitude", "longitude"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (t *SunTrigger) GetConfigSchema() node.ConfigSchema { return sunConfigSchema() }

// SunTriggerInfo returns the node type info for registration.
func SunTriggerInfo() node.NodeTypeInfo {
	schema := sunConfigSchema()
	return node.NodeTypeInfo{
		Type:        "trigger-sun",
		Name:        "Sun",
		Description: "Trigger at sunrise, sunset, dawn or dusk",
		Documentation: `## Sun

Fires every day at sunrise, sunset or civil dawn / dusk at the given place,
moved by an offset in minutes (−30: half an hour before). Times are computed
on the device (NOAA algorithm, within about two minutes); no network is used.
Where the event does not happen on a day (polar day or night), that day is
skipped.

## Output

` + "`{ event, time, latitude, longitude }`" + ` (time in UTC, RFC 3339).

## Example

Sunset −15 min → Zigbee Set *on* for the porch plug.
`,
		Category: types.NodeCategoryInput,
		Risk:     node.RiskSafe,
		Outputs:  sunOutputs,
		Config:   &schema,
		Icon:     "sunset",
	}
}

func init() {
	info := SunTriggerInfo()
	if err := node.Register(info, NewSunTrigger); err != nil {
		panic("failed to register sun trigger: " + err.Error())
	}
}
