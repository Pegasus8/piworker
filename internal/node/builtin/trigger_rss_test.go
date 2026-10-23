package builtin

import (
	"testing"

	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRSS(t *testing.T, config map[string]interface{}) *RSSTrigger {
	t.Helper()
	n, err := NewRSSTrigger(config)
	require.NoError(t, err)
	return n.(*RSSTrigger)
}

func mustParseRSS(t *testing.T, s string) *gofeed.Feed {
	t.Helper()
	feed, err := gofeed.NewParser().ParseString(s)
	require.NoError(t, err)
	return feed
}

const rss2 = `<rss version="2.0"><channel><title>T</title>
<item><guid>1</guid><title>First</title><link>http://e/1</link></item>
<item><guid>2</guid><title>Second</title><link>http://e/2</link></item>
</channel></rss>`

const rss3 = `<rss version="2.0"><channel><title>T</title>
<item><guid>1</guid><title>First</title><link>http://e/1</link></item>
<item><guid>2</guid><title>Second</title><link>http://e/2</link></item>
<item><guid>3</guid><title>Third</title><link>http://e/3</link></item>
</channel></rss>`

func TestRSSNewItemsDeduplicates(t *testing.T) {
	tr := newRSS(t, map[string]interface{}{"url": "http://example/feed"})

	first := tr.newItems(mustParseRSS(t, rss2))
	assert.Len(t, first, 2, "all items new on first sight")

	second := tr.newItems(mustParseRSS(t, rss2))
	assert.Empty(t, second, "already-seen items are not re-emitted")

	third := tr.newItems(mustParseRSS(t, rss3))
	require.Len(t, third, 1, "only the newly-added item")
	assert.Equal(t, "3", third[0].GUID)
}

func TestRSSItemIDPrefersGUIDThenLink(t *testing.T) {
	assert.Equal(t, "g", itemID(&gofeed.Item{GUID: "g", Link: "l", Title: "t"}))
	assert.Equal(t, "l", itemID(&gofeed.Item{Link: "l", Title: "t"}))
	assert.Equal(t, "t", itemID(&gofeed.Item{Title: "t"}))
}

func TestRSSBuildPayload(t *testing.T) {
	p := rssItemPayload(&gofeed.Item{GUID: "1", Title: "Hello", Link: "http://e/1", Description: "d"})
	assert.Equal(t, "Hello", p["title"])
	assert.Equal(t, "http://e/1", p["link"])
	assert.Equal(t, "d", p["description"])
	assert.Equal(t, "1", p["guid"])
}

func TestRSSRequiresURL(t *testing.T) {
	_, err := NewRSSTrigger(map[string]interface{}{})
	assert.Error(t, err)
}
