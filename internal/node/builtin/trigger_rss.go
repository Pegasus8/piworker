package builtin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/mmcdole/gofeed"
	"github.com/rs/zerolog/log"
)

const (
	rssMinInterval  = 10 * time.Second
	rssMaxBodyBytes = 5 << 20 // 5 MiB
	rssMinSeen      = 1000
)

// RSSTrigger polls an RSS/Atom feed and emits a message per newly-seen item.
type RSSTrigger struct {
	*node.BaseNode

	url          string
	interval     time.Duration
	allowPrivate bool

	client *http.Client
	parser *gofeed.Parser

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
	// seen maps item id -> the poll sequence it was last present in. It is
	// pruned to the most recently present ids (see newItems), so it doesn't
	// grow with every item a feed ever published.
	seen    map[string]uint64
	pollSeq uint64
	seeded  bool
}

// NewRSSTrigger creates an RSSTrigger from configuration.
func NewRSSTrigger(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	url := base.GetConfigString("url", "")
	if url == "" {
		return nil, fmt.Errorf("url is required")
	}
	interval := time.Duration(base.GetConfigInt("intervalSec", 300)) * time.Second
	if interval < rssMinInterval {
		interval = rssMinInterval
	}
	allowPrivate := base.GetConfigBool("allowPrivate", false)

	return &RSSTrigger{
		BaseNode:     base,
		url:          url,
		interval:     interval,
		allowPrivate: allowPrivate,
		client:       newGuardedHTTPClient(30*time.Second, allowPrivate),
		parser:       gofeed.NewParser(),
		stopCh:       make(chan struct{}),
		seen:         make(map[string]uint64),
	}, nil
}

// Process is unused for trigger nodes.
func (t *RSSTrigger) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}

// itemID returns a stable identity for a feed item, preferring GUID, then link,
// then title.
func itemID(item *gofeed.Item) string {
	switch {
	case item.GUID != "":
		return item.GUID
	case item.Link != "":
		return item.Link
	default:
		return item.Title
	}
}

// newItems returns the feed items not seen before and records every item of
// the feed as seen. The seen set keeps at most max(rssMinSeen, 2*feed size)
// ids, dropping those absent from the feed for longest, so it stays bounded
// while still tolerating a feed that briefly omits items.
func (t *RSSTrigger) newItems(feed *gofeed.Feed) []*gofeed.Item {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pollSeq++
	var fresh []*gofeed.Item
	for _, item := range feed.Items {
		id := itemID(item)
		if _, ok := t.seen[id]; !ok {
			fresh = append(fresh, item)
		}
		t.seen[id] = t.pollSeq
	}
	t.pruneSeen(max(rssMinSeen, 2*len(feed.Items)))
	return fresh
}

// pruneSeen drops the least recently present ids beyond limit. Called with
// t.mu held.
func (t *RSSTrigger) pruneSeen(limit int) {
	if len(t.seen) <= limit {
		return
	}
	seqs := make([]uint64, 0, len(t.seen))
	for _, seq := range t.seen {
		seqs = append(seqs, seq)
	}
	slices.Sort(seqs)
	cutoff := seqs[len(seqs)-limit] // keep ids last seen at or after this poll
	for id, seq := range t.seen {
		if seq < cutoff {
			delete(t.seen, id)
		}
	}
	// Ties at the cutoff can leave us above limit; trim arbitrarily among them.
	for id, seq := range t.seen {
		if len(t.seen) <= limit {
			break
		}
		if seq == cutoff {
			delete(t.seen, id)
		}
	}
}

// pollItems returns the items to emit for a successfully fetched feed. The
// first successful fetch only seeds the seen set, so deploying a flow (or
// booting before the network is up, when the first attempts fail) doesn't
// replay the whole backlog.
func (t *RSSTrigger) pollItems(feed *gofeed.Feed) []*gofeed.Item {
	fresh := t.newItems(feed)
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.seeded {
		t.seeded = true
		return nil
	}
	return fresh
}

func rssItemPayload(item *gofeed.Item) map[string]interface{} {
	p := map[string]interface{}{
		"guid":        item.GUID,
		"title":       item.Title,
		"link":        item.Link,
		"description": item.Description,
		"content":     item.Content,
	}
	if item.Author != nil {
		p["author"] = item.Author.Name
	}
	if item.PublishedParsed != nil {
		p["published"] = item.PublishedParsed.UnixMilli()
	}
	return p
}

// fetch downloads and parses the feed using the SSRF-guarded client.
func (t *RSSTrigger) fetch(ctx context.Context) (*gofeed.Feed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("feed returned HTTP %d", resp.StatusCode)
	}
	body := io.LimitReader(resp.Body, rssMaxBodyBytes)
	return t.parser.Parse(body)
}

// Start begins polling the feed.
func (t *RSSTrigger) Start(ctx context.Context, out chan<- *types.Message) error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return fmt.Errorf("trigger already running")
	}
	t.running = true
	t.stopCh = make(chan struct{})
	t.mu.Unlock()

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Bytes("stack", debug.Stack()).Msg("rss trigger goroutine panicked")
			}
		}()

		ticker := time.NewTicker(t.interval)
		defer ticker.Stop()

		// Poll right away; the first successful poll only seeds the seen set.
		if feed, err := t.fetch(ctx); err != nil {
			log.Warn().Err(err).Str("url", t.url).Msg("rss initial fetch failed; will retry")
		} else {
			t.pollItems(feed)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.stopCh:
				return
			case <-ticker.C:
				feed, err := t.fetch(ctx)
				if err != nil {
					log.Warn().Err(err).Str("url", t.url).Msg("rss fetch failed")
					continue
				}
				for _, item := range t.pollItems(feed) {
					msg := types.NewMessage(rssItemPayload(item), types.DataTypeObject)
					msg.SourcePort = "output"
					msg.Topic = "rss"
					select {
					case <-ctx.Done():
						return
					case <-t.stopCh:
						return
					case out <- msg:
					default:
						metrics.IncrementMessagesDropped()
						log.Warn().Str("title", item.Title).Msg("rss item dropped: output channel full")
					}
				}
			}
		}
	}()
	return nil
}

// Stop stops polling.
func (t *RSSTrigger) Stop() error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}
	close(t.stopCh)
	t.running = false
	t.mu.Unlock()
	t.wg.Wait()
	return nil
}

// Ports returns the port definitions.
func (t *RSSTrigger) Ports() (inputs []types.Port, outputs []types.Port) {
	return nil, []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}}
}

// Validate checks the configuration.
func (t *RSSTrigger) Validate() error {
	if t.url == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

func rssTriggerConfigSchema() node.ConfigSchema {
	minInterval := float64(10)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"url": {
				Type:        "string",
				Title:       "Feed URL",
				Description: "RSS or Atom feed URL to poll",
			},
			"intervalSec": {
				Type:        "number",
				Title:       "Poll interval (s)",
				Description: "How often to poll the feed, in seconds",
				Default:     300,
				Minimum:     &minInterval,
			},
			"allowPrivate": {
				Type:        "boolean",
				Title:       "Allow private hosts",
				Description: "Permit fetching from private/loopback addresses (SSRF risk)",
				Default:     false,
			},
		},
		Required: []string{"url"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (t *RSSTrigger) GetConfigSchema() node.ConfigSchema {
	return rssTriggerConfigSchema()
}

// RSSTriggerInfo returns the node type info for registration.
func RSSTriggerInfo() node.NodeTypeInfo {
	schema := rssTriggerConfigSchema()
	return node.NodeTypeInfo{
		Type:        "trigger-rss",
		Name:        "RSS Feed",
		Description: "Trigger when an RSS/Atom feed gets a new item",
		Documentation: `## RSS Feed

Polls an RSS or Atom feed and emits one message per newly-seen item. The first
successful poll seeds the seen-set without emitting, so deploying a flow (or booting
before the network is up) doesn't replay the
existing backlog.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| url | string | - | Feed URL (RSS or Atom) |
| intervalSec | number | 300 | Poll interval in seconds (min 10) |
| allowPrivate | boolean | false | Allow private/loopback hosts |

## Output payload

` + "`{ guid, title, link, description, content, author?, published? }`" + `

## Use Cases

- React to new blog/podcast episodes
- Watch a status feed and notify on new entries
`,
		Category: types.NodeCategoryInput,
		Inputs:   nil,
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}},
		Config:   &schema,
		Icon:     "rss",
	}
}

func init() {
	info := RSSTriggerInfo()
	if err := node.Register(info, NewRSSTrigger); err != nil {
		panic("failed to register rss trigger: " + err.Error())
	}
}
