package telekit

import (
	"context"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// floodWaitMiddleware automatically sleeps and retries when Telegram returns
// a FLOOD_WAIT error, instead of propagating the error to the caller.
type floodWaitMiddleware struct{}

func (f floodWaitMiddleware) Handle(next tg.Invoker) telegram.InvokeFunc {
	return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
		for {
			err := next.Invoke(ctx, input, output)
			if err == nil {
				return nil
			}

			waited, waitErr := tgerr.FloodWait(ctx, err)
			if !waited {
				return waitErr
			}
		}
	}
}
