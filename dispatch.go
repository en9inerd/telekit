package telekit

import (
	"context"
	"strings"

	"github.com/gotd/td/tg"
)

func (b *Bot) registerDispatcherHandlers() {
	b.dispatcher.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		msg, ok := u.Message.(*tg.Message)
		if !ok {
			return nil
		}
		return b.handleMessage(ctx, msg, u, e)
	})

	b.dispatcher.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		msg, ok := u.Message.(*tg.Message)
		if !ok {
			return nil
		}
		return b.handleMessage(ctx, msg, u, e)
	})

	b.dispatcher.OnEditChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateEditChannelMessage) error {
		msg, ok := u.Message.(*tg.Message)
		if !ok {
			return nil
		}
		return b.handleEdit(ctx, msg, u, e)
	})

	b.dispatcher.OnEditMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateEditMessage) error {
		msg, ok := u.Message.(*tg.Message)
		if !ok {
			return nil
		}
		return b.handleEdit(ctx, msg, u, e)
	})

	b.dispatcher.OnBotCallbackQuery(func(ctx context.Context, _ tg.Entities, u *tg.UpdateBotCallbackQuery) error {
		return b.handleCallback(ctx, u)
	})

	b.dispatcher.OnDeleteChannelMessages(func(ctx context.Context, _ tg.Entities, u *tg.UpdateDeleteChannelMessages) error {
		return b.handleDelete(ctx, u.Messages, 0, u.ChannelID)
	})

	b.dispatcher.OnDeleteMessages(func(ctx context.Context, _ tg.Entities, u *tg.UpdateDeleteMessages) error {
		return b.handleDelete(ctx, u.Messages, 0, 0)
	})
}

func (b *Bot) handleMessage(ctx context.Context, msg *tg.Message, update tg.UpdateClass, entities tg.Entities) error {
	if msg.Out {
		return nil
	}

	if b.albumCollector.add(ctx, msg, entities) {
		return nil
	}

	botCtx := &Context{
		Context:  ctx,
		bot:      b,
		message:  msg,
		update:   update,
		entities: entities,
	}

	if strings.HasPrefix(msg.Message, "/") {
		if err := b.handleCommand(botCtx); err != nil {
			b.config.Logger.Error("command handler error", "error", err)
		}
		return nil
	}

	b.mu.RLock()
	handlers := b.messageHandlers
	b.mu.RUnlock()

	for _, h := range handlers {
		if h.filter.matches(botCtx) {
			if err := h.fn(botCtx); err != nil {
				b.config.Logger.Error("message handler error", "error", err)
			}
		}
	}

	return nil
}

func (b *Bot) handleEdit(ctx context.Context, msg *tg.Message, update tg.UpdateClass, entities tg.Entities) error {
	botCtx := &Context{
		Context:  ctx,
		bot:      b,
		message:  msg,
		update:   update,
		entities: entities,
	}

	b.mu.RLock()
	handlers := b.editHandlers
	b.mu.RUnlock()

	for _, h := range handlers {
		if h.filter.matches(botCtx) {
			if err := h.fn(botCtx); err != nil {
				b.config.Logger.Error("edit handler error", "error", err)
			}
		}
	}

	return nil
}

func (b *Bot) handleCommand(ctx *Context) error {
	text := ctx.Text()
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil
	}

	cmdName := strings.TrimPrefix(parts[0], "/")
	if idx := strings.Index(cmdName, "@"); idx > 0 {
		cmdName = cmdName[:idx]
	}

	b.config.Logger.Debug("received command",
		"command", cmdName,
		"sender_id", ctx.SenderID(),
		"chat_id", ctx.ChatID(),
		"text", text)

	userID := ctx.SenderID()

	b.mu.RLock()
	handlers := b.commandHandlers
	b.mu.RUnlock()

	for _, h := range handlers {
		if h.name == cmdName {
			if !h.filter.matches(ctx) {
				b.config.Logger.Debug("command filter not matched",
					"command", cmdName,
					"sender_id", ctx.SenderID(),
					"filter_users", h.filter.Users)
				continue
			}

			if userID != 0 {
				if !b.commandLock.TryAcquire(userID, h.locked) {
					b.config.Logger.Debug("command blocked by lock",
						"command", cmdName,
						"sender_id", userID)
					return nil
				}
				if h.locked {
					defer b.commandLock.Unlock(userID)
				}
			}

			params, err := parseParams(text, h.params)
			if err != nil {
				if ctx.SenderID() != 0 {
					_ = ctx.SendTo(ctx.SenderID(), "Error: "+err.Error())
				}
				return nil
			}
			ctx.params = params

			return h.fn(ctx)
		}
	}

	return nil
}

func (b *Bot) handleAlbum(ctx context.Context, messages []*tg.Message, entities tg.Entities) {
	if len(messages) == 0 {
		return
	}

	botCtx := &Context{
		Context:  ctx,
		bot:      b,
		message:  messages[0],
		messages: messages,
		entities: entities,
	}

	b.mu.RLock()
	handlers := b.albumHandlers
	b.mu.RUnlock()

	for _, h := range handlers {
		if h.filter.matches(botCtx) {
			if err := h.fn(botCtx); err != nil {
				b.config.Logger.Error("album handler error", "error", err)
			}
		}
	}
}

func (b *Bot) handleCallback(ctx context.Context, query *tg.UpdateBotCallbackQuery) error {
	data := string(query.Data)

	var chatID int64
	var msgID int
	if peer := query.Peer; peer != nil {
		switch p := peer.(type) {
		case *tg.PeerUser:
			chatID = p.UserID
		case *tg.PeerChat:
			chatID = p.ChatID
		case *tg.PeerChannel:
			chatID = p.ChannelID
		}
	}
	msgID = query.MsgID

	cbCtx := &CallbackContext{
		Context: ctx,
		bot:     b,
		query:   query,
		data:    data,
		userID:  query.UserID,
		msgID:   msgID,
		chatID:  chatID,
	}

	b.mu.RLock()
	handlers := b.callbackHandlers
	b.mu.RUnlock()

	for _, h := range handlers {
		if h.filter.matches(cbCtx) {
			if err := h.fn(cbCtx); err != nil {
				b.config.Logger.Error("callback handler error", "error", err)
			}
		}
	}

	return nil
}

func (b *Bot) handleDelete(ctx context.Context, messageIDs []int, chatID, channelID int64) error {
	delCtx := &DeleteContext{
		Context:    ctx,
		bot:        b,
		messageIDs: messageIDs,
		chatID:     chatID,
		channelID:  channelID,
	}

	b.mu.RLock()
	handlers := b.deleteHandlers
	b.mu.RUnlock()

	for _, h := range handlers {
		if h.filter.matches(delCtx) {
			if err := h.fn(delCtx); err != nil {
				b.config.Logger.Error("delete handler error", "error", err)
			}
		}
	}

	return nil
}
