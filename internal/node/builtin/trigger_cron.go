package builtin

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
)

// cronParser parses standard 5-field cron expressions (minute hour dom month dow).
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// CronTrigger fires messages based on a cron schedule.
type CronTrigger struct {
	*node.BaseNode

	expression string
	cron       *cron.Cron
	mu         sync.Mutex
	running    bool
	count      uint64
}

// NewCronTrigger creates a new CronTrigger with the given configuration.
func NewCronTrigger(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	expression := base.GetConfigString("expression", "*/5 * * * *") // Default: every 5 minutes

	// Validate cron expression early to fail fast
	if _, err := cronParser.Parse(expression); err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", expression, err)
	}

	trigger := &CronTrigger{
		BaseNode:   base,
		expression: expression,
	}

	return trigger, nil
}

// Process is not used for trigger nodes but must be implemented.
func (t *CronTrigger) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	// Trigger nodes don't process incoming messages
	return nil, nil
}

// Ports returns the port definitions for this node.
func (t *CronTrigger) Ports() (inputs []types.Port, outputs []types.Port) {
	return nil, []types.Port{
		{
			ID:       "output",
			Name:     "Output",
			DataType: types.DataTypeObject,
			Multiple: true,
		},
	}
}

// Validate checks if the cron expression is valid.
func (t *CronTrigger) Validate() error {
	if _, err := cronParser.Parse(t.expression); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", t.expression, err)
	}
	return nil
}

// Start begins the cron trigger.
func (t *CronTrigger) Start(ctx context.Context, out chan<- *types.Message) error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return fmt.Errorf("trigger already running")
	}

	// Validate the expression
	if err := t.Validate(); err != nil {
		t.mu.Unlock()
		return err
	}

	t.cron = cron.New(cron.WithParser(cronParser))

	_, err := t.cron.AddFunc(t.expression, func() {
		t.mu.Lock()
		t.count++
		count := t.count
		t.mu.Unlock()

		msg := types.NewMessage(map[string]interface{}{
			"timestamp":  time.Now().UnixMilli(),
			"count":      count,
			"expression": t.expression,
		}, types.DataTypeObject)
		msg.SourcePort = "output"
		msg.Topic = "cron"

		select {
		case out <- msg:
		default:
			metrics.IncrementMessagesDropped()
			log.Warn().Uint64("count", count).Msg("cron tick dropped: output channel full")
		}
	})

	if err != nil {
		t.mu.Unlock()
		return fmt.Errorf("failed to add cron job: %w", err)
	}

	t.cron.Start()
	t.running = true
	t.mu.Unlock()

	// Watch for context cancellation
	go func() {
		<-ctx.Done()
		t.Stop()
	}()

	return nil
}

// Stop stops the cron trigger.
func (t *CronTrigger) Stop() error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}

	// Snapshot and clear shared state under the lock, then release it BEFORE
	// waiting for in-flight jobs to drain. The cron job func also acquires
	// t.mu (to bump count), so blocking on cron.Stop()'s context while holding
	// the lock would deadlock: the running job could never acquire t.mu, so the
	// jobWaiter would never complete and Stop() would hang forever.
	c := t.cron
	t.cron = nil
	t.running = false
	t.mu.Unlock()

	if c != nil {
		<-c.Stop().Done() // wait for running jobs to complete (lock released)
	}

	return nil
}

// GetConfigSchema returns the configuration schema for UI.
func (t *CronTrigger) GetConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"expression": {
				Type:        "string",
				Title:       "Cron Expression",
				Description: "Standard cron expression (minute hour day month weekday)",
				Default:     "*/5 * * * *",
			},
		},
		Required: []string{"expression"},
	}
}

// CronTriggerInfo returns the node type info for registration.
func CronTriggerInfo() node.NodeTypeInfo {
	return node.NodeTypeInfo{
		Type:        "trigger-cron",
		Name:        "Cron Schedule",
		Description: "Triggers based on a cron schedule expression",
		Documentation: `## Cron Schedule

Fires messages based on standard cron expressions. Ideal for scheduled tasks that need to run at specific times.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| expression | string | */5 * * * * | Standard 5-field cron expression |

## Cron Expression Format

` + "```" + `
┌───────────── minute (0-59)
│ ┌───────────── hour (0-23)
│ │ ┌───────────── day of month (1-31)
│ │ │ ┌───────────── month (1-12)
│ │ │ │ ┌───────────── day of week (0-6, Sun=0)
│ │ │ │ │
* * * * *
` + "```" + `

## Common Patterns

| Expression | Description |
|------------|-------------|
| * * * * * | Every minute |
| */5 * * * * | Every 5 minutes |
| 0 * * * * | Every hour |
| 0 0 * * * | Daily at midnight |
| 0 9 * * 1 | Every Monday at 9 AM |
| 0 0 1 * * | First day of every month |

## Output

Each trigger produces:

- **payload.timestamp** - Unix timestamp when triggered
- **payload.count** - Number of triggers since start
- **payload.expression** - The cron expression used
- **topic** - Always set to "cron"

## Use Cases

- Daily reports at specific times
- Weekly cleanup tasks
- Monthly billing operations
- Business hours only processing
`,
		Category: types.NodeCategoryInput,
		Inputs:   nil,
		Outputs: []types.Port{
			{
				ID:       "output",
				Name:     "Output",
				DataType: types.DataTypeObject,
				Multiple: true,
			},
		},
		Icon: "schedule",
	}
}

func init() {
	info := CronTriggerInfo()
	if err := node.Register(info, NewCronTrigger); err != nil {
		panic("failed to register cron trigger: " + err.Error())
	}
}
