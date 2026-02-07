package telekit

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/gotd/td/tg"
)

// albumCollector collects grouped messages (albums) and fires a callback
// when the album is complete.
type albumCollector struct {
	mu       sync.Mutex
	albums   map[int64][]*tg.Message  // groupedID -> messages
	entities map[int64]tg.Entities    // groupedID -> entities (from first message)
	timers   map[int64]*time.Timer
	timeout  time.Duration
	callback func(ctx context.Context, messages []*tg.Message, entities tg.Entities)
}

func newAlbumCollector(timeout time.Duration, callback func(ctx context.Context, messages []*tg.Message, entities tg.Entities)) *albumCollector {
	return &albumCollector{
		albums:   make(map[int64][]*tg.Message),
		entities: make(map[int64]tg.Entities),
		timers:   make(map[int64]*time.Timer),
		timeout:  timeout,
		callback: callback,
	}
}

// Returns true if the message was collected into an album (should not be processed individually).
func (c *albumCollector) add(ctx context.Context, msg *tg.Message, entities tg.Entities) bool {
	if msg.GroupedID == 0 {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	groupID := msg.GroupedID
	if len(c.albums[groupID]) == 0 {
		c.entities[groupID] = entities
	}
	c.albums[groupID] = append(c.albums[groupID], msg)

	if timer, ok := c.timers[groupID]; ok {
		timer.Stop()
	}

	c.timers[groupID] = time.AfterFunc(c.timeout, func() {
		c.flush(ctx, groupID)
	})

	return true
}

func (c *albumCollector) flush(ctx context.Context, groupID int64) {
	c.mu.Lock()
	messages := c.albums[groupID]
	entities := c.entities[groupID]
	delete(c.albums, groupID)
	delete(c.entities, groupID)
	delete(c.timers, groupID)
	c.mu.Unlock()

	if len(messages) == 0 {
		return
	}

	// Sort by message ID to ensure correct order
	slices.SortFunc(messages, func(a, b *tg.Message) int {
		return cmp.Compare(a.ID, b.ID)
	})

	c.callback(ctx, messages, entities)
}

// stop cancels all pending album timers.
func (c *albumCollector) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, timer := range c.timers {
		timer.Stop()
	}
	c.albums = make(map[int64][]*tg.Message)
	c.entities = make(map[int64]tg.Entities)
	c.timers = make(map[int64]*time.Timer)
}
