package builtin

import (
	"fmt"
	"strings"
	"sync"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/host/v3"
)

// periph host initialization is process-wide and done once, lazily, so the
// binary still runs on non-Pi hosts (where the buses simply aren't found).
var (
	periphInitOnce sync.Once
	periphInitErr  error
)

// periphInit runs periph's host.Init() exactly once. It registers every bus
// driver (GPIO, I2C, SPI, 1-Wire, …), so it's shared by all peripheral nodes,
// not just GPIO.
func periphInit() error {
	periphInitOnce.Do(func() { _, periphInitErr = host.Init() })
	return periphInitErr
}

// gpioPin resolves a named pin (e.g. "GPIO17"), initializing the host on first
// use. It returns a clear error on non-Pi hosts where the pin doesn't exist.
func gpioPin(name string) (gpio.PinIO, error) {
	if err := periphInit(); err != nil {
		return nil, fmt.Errorf("GPIO host init failed: %w", err)
	}
	p := gpioreg.ByName(name)
	if p == nil {
		return nil, fmt.Errorf("GPIO pin %q not found (no GPIO on this host?)", name)
	}
	return p, nil
}

// parseLevel maps a friendly value to a gpio.Level (defaults to Low).
func parseLevel(s string) gpio.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high", "1", "on", "true":
		return gpio.High
	default:
		return gpio.Low
	}
}

// parseEdge maps a friendly edge name to a gpio.Edge (defaults to BothEdges).
func parseEdge(s string) gpio.Edge {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "rising":
		return gpio.RisingEdge
	case "falling":
		return gpio.FallingEdge
	default:
		return gpio.BothEdges
	}
}

// parsePull maps a friendly pull name to a gpio.Pull (defaults to Float).
func parsePull(s string) gpio.Pull {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "up":
		return gpio.PullUp
	case "down":
		return gpio.PullDown
	default:
		return gpio.Float
	}
}

// truthy interprets a payload value as a boolean for payload-driven outputs.
func truthy(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s != "" && s != "false" && s != "0" && s != "off"
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return true
	}
}
