package builtin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rssWithItems(ids ...int) string {
	var b strings.Builder
	b.WriteString(`<rss version="2.0"><channel><title>T</title>`)
	for _, id := range ids {
		fmt.Fprintf(&b, `<item><guid>%d</guid><title>Item %d</title></item>`, id, id)
	}
	b.WriteString(`</channel></rss>`)
	return b.String()
}

func TestRSSSeenSetIsBounded(t *testing.T) {
	tr := newRSS(t, map[string]interface{}{"url": "http://example/feed"})
	for i := 0; i < 5000; i++ {
		fresh := tr.newItems(mustParseRSS(t, rssWithItems(i, i+1)))
		assert.LessOrEqual(t, len(fresh), 2)
	}
	assert.LessOrEqual(t, len(tr.seen), rssMinSeen, "seen set must not grow with every item ever published")

	// Items still in the feed stay remembered and are not re-emitted.
	assert.Empty(t, tr.newItems(mustParseRSS(t, rssWithItems(4999, 5000))))
}

func TestRSSSeenSetKeepsWholeLargeFeed(t *testing.T) {
	tr := newRSS(t, map[string]interface{}{"url": "http://example/feed"})
	ids := make([]int, 0, 1500)
	for i := 0; i < 1500; i++ {
		ids = append(ids, i)
	}
	feed := mustParseRSS(t, rssWithItems(ids...))
	assert.Len(t, tr.newItems(feed), 1500)
	assert.Empty(t, tr.newItems(feed), "a feed larger than the floor must not be re-emitted")
}

// Booting before the network is up: the first fetch fails, so seeding must
// happen on the first successful fetch instead of emitting the backlog.
func TestRSSSeedsOnFirstSuccessfulFetch(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n := calls.Add(1); {
		case n == 1:
			http.Error(w, "<html>bad gateway</html>", http.StatusBadGateway)
		case n == 2:
			_, _ = w.Write([]byte(rssWithItems(1, 2, 3)))
		default:
			_, _ = w.Write([]byte(rssWithItems(1, 2, 3, 4)))
		}
	}))
	defer srv.Close()

	tr := newRSS(t, map[string]interface{}{"url": srv.URL, "allowPrivate": true})
	tr.interval = 20 * time.Millisecond
	out := make(chan *types.Message, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, tr.Start(ctx, out))
	defer func() { _ = tr.Stop() }()

	select {
	case msg := <-out:
		assert.Equal(t, "4", msg.Payload.(map[string]interface{})["guid"], "only the item published after seeding")
	case <-time.After(5 * time.Second):
		t.Fatal("no item emitted")
	}
	select {
	case msg := <-out:
		t.Fatalf("unexpected extra item %v", msg.Payload)
	case <-time.After(100 * time.Millisecond):
	}
}
