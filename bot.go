package telekit

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/updates"
	updhook "github.com/gotd/td/telegram/updates/hook"
	"github.com/gotd/td/tg"
)

// Bot is the main Telegram bot client.
type Bot struct {
	config     Config
	client     *telegram.Client
	api        *tg.Client
	dispatcher tg.UpdateDispatcher
	gaps       *updates.Manager

	// Handlers
	mu               sync.RWMutex
	messageHandlers  []handler
	editHandlers     []handler
	deleteHandlers   []deleteHandler
	callbackHandlers []callbackHandler
	commandHandlers  []commandHandler
	albumHandlers    []handler

	// Command locking
	commandLock *CommandLock

	// Album collector
	albumCollector *albumCollector

	// Lifecycle callbacks
	onReady func(ctx context.Context)

	// State
	running atomic.Bool
	selfID  int64
}

// New creates a new Bot with the given configuration.
func New(cfg Config) (*Bot, error) {
	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(cfg.SessionDir, 0700); err != nil {
		return nil, err
	}

	dispatcher := tg.NewUpdateDispatcher()

	sessionStorage := &session.FileStorage{
		Path: filepath.Join(cfg.SessionDir, "session"),
	}

	gaps := updates.New(updates.Config{
		Handler: &dispatcher,
	})

	client := telegram.NewClient(cfg.APIID, cfg.APIHash, telegram.Options{
		Logger:        cfg.zapLogger(),
		UpdateHandler: gaps,
		Middlewares: []telegram.Middleware{
			updhook.UpdateHook(gaps.Handle),
		},
		Device: telegram.DeviceConfig{
			DeviceModel:    cfg.DeviceModel,
			SystemVersion:  cfg.SystemVersion,
			AppVersion:     cfg.AppVersion,
			LangCode:       cfg.LangCode,
			SystemLangCode: cfg.SystemLangCode,
		},
		SessionStorage: sessionStorage,
	})

	bot := &Bot{
		config:      cfg,
		client:      client,
		dispatcher:  dispatcher,
		gaps:        gaps,
		commandLock: NewCommandLock(),
	}

	bot.albumCollector = newAlbumCollector(cfg.AlbumTimeout, bot.handleAlbum)
	bot.registerDispatcherHandlers()

	return bot, nil
}

// OnReady sets a callback that's called when the bot is connected and ready.
func (b *Bot) OnReady(fn func(ctx context.Context)) {
	b.onReady = fn
}

// OnMessage registers a handler for new messages.
func (b *Bot) OnMessage(filter Filter, fn HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messageHandlers = append(b.messageHandlers, handler{fn: fn, filter: filter})
}

// OnChannelPost registers a handler for new channel posts.
func (b *Bot) OnChannelPost(channelID int64, fn HandlerFunc) {
	b.OnMessage(Filter{
		Chats:    []int64{channelID},
		Incoming: true,
	}, fn)
}

// OnPrivateMessage registers a handler for private messages from specific users.
func (b *Bot) OnPrivateMessage(userIDs []int64, fn HandlerFunc) {
	b.OnMessage(Filter{
		Users:    userIDs,
		Incoming: true,
	}, fn)
}

// OnEdit registers a handler for edited messages.
func (b *Bot) OnEdit(filter Filter, fn HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.editHandlers = append(b.editHandlers, handler{fn: fn, filter: filter})
}

// OnChannelEdit registers a handler for edited channel posts.
func (b *Bot) OnChannelEdit(channelID int64, fn HandlerFunc) {
	b.OnEdit(Filter{
		Chats: []int64{channelID},
	}, fn)
}

// OnAlbum registers a handler for albums (grouped media).
func (b *Bot) OnAlbum(filter Filter, fn HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.albumHandlers = append(b.albumHandlers, handler{fn: fn, filter: filter})
}

// CommandDef defines a command with its metadata.
type CommandDef struct {
	// Name is the command name without the leading slash.
	Name string

	// Description is shown in the bot's command menu.
	Description string

	// Params defines the parameter schema for validation.
	Params Params

	// Locked enables mutual exclusion for this command.
	// When true, this command blocks other locked commands for the same user.
	Locked bool

	// Scope defines where the command is available (default: ScopeDefault).
	Scope CommandScope

	// LangCode is the language code for this command's description.
	LangCode string
}

// Command registers a command handler with optional parameter schema.
func (b *Bot) Command(name string, params Params, fn HandlerFunc) {
	b.CommandWithFilter(CommandDef{Name: name, Params: params}, Filter{Incoming: true}, fn)
}

// CommandWithDesc registers a command with description (for menu sync).
func (b *Bot) CommandWithDesc(def CommandDef, fn HandlerFunc) {
	b.CommandWithFilter(def, Filter{Incoming: true}, fn)
}

// CommandWithFilter registers a command handler with a custom filter.
func (b *Bot) CommandWithFilter(def CommandDef, filter Filter, fn HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.commandHandlers = append(b.commandHandlers, commandHandler{
		name:        def.Name,
		description: def.Description,
		params:      def.Params,
		fn:          fn,
		filter:      filter,
		locked:      def.Locked,
		scope:       def.Scope,
		langCode:    def.LangCode,
	})
}

// CommandFrom registers a command handler that only responds to specific users.
func (b *Bot) CommandFrom(name string, params Params, userIDs []int64, fn HandlerFunc) {
	b.CommandWithFilter(CommandDef{Name: name, Params: params}, Filter{
		Users:    userIDs,
		Incoming: true,
	}, fn)
}

// LockedCommand registers a command with mutual exclusion.
func (b *Bot) LockedCommand(name string, params Params, fn HandlerFunc) {
	b.CommandWithFilter(CommandDef{Name: name, Params: params, Locked: true}, Filter{Incoming: true}, fn)
}

// LockedCommandWithDesc registers a locked command with description.
func (b *Bot) LockedCommandWithDesc(def CommandDef, fn HandlerFunc) {
	def.Locked = true
	b.CommandWithFilter(def, Filter{Incoming: true}, fn)
}

// LockedCommandFrom registers a locked command for specific users.
func (b *Bot) LockedCommandFrom(name string, params Params, userIDs []int64, fn HandlerFunc) {
	b.CommandWithFilter(CommandDef{Name: name, Params: params, Locked: true}, Filter{
		Users:    userIDs,
		Incoming: true,
	}, fn)
}

// OnCallback registers a handler for callback queries (inline button clicks).
func (b *Bot) OnCallback(filter CallbackFilter, fn CallbackFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.callbackHandlers = append(b.callbackHandlers, callbackHandler{fn: fn, filter: filter})
}

// OnCallbackPrefix registers a handler for callback queries with a specific data prefix.
func (b *Bot) OnCallbackPrefix(prefix string, fn CallbackFunc) {
	b.OnCallback(CallbackFilter{DataPrefix: prefix}, fn)
}

// OnDelete registers a handler for deleted messages.
func (b *Bot) OnDelete(filter DeleteFilter, fn DeleteFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deleteHandlers = append(b.deleteHandlers, deleteHandler{fn: fn, filter: filter})
}

// OnChannelDelete registers a handler for deleted channel messages.
func (b *Bot) OnChannelDelete(channelID int64, fn DeleteFunc) {
	b.OnDelete(DeleteFilter{Chats: []int64{channelID}}, fn)
}

// Run starts the bot and blocks until the context is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	if !b.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	defer b.running.Store(false)

	return b.client.Run(ctx, func(ctx context.Context) error {
		defer b.albumCollector.stop()

		status, err := b.client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !status.Authorized {
			if _, err := b.client.Auth().Bot(ctx, b.config.BotToken); err != nil {
				return err
			}
		}

		self, err := b.client.Self(ctx)
		if err != nil {
			return err
		}
		b.selfID = self.ID
		b.api = tg.NewClient(b.client)

		if b.config.ProfilePhotoURL != "" {
			if err := b.SetProfilePhoto(ctx, b.config.ProfilePhotoURL); err != nil {
				b.config.Logger.Warn("failed to set profile photo", "error", err)
			}
		}

		if b.config.BotInfo != nil {
			if err := b.UpdateBotInfo(ctx, *b.config.BotInfo); err != nil {
				b.config.Logger.Warn("failed to update bot info", "error", err)
			}
		}

		if b.onReady != nil {
			b.onReady(ctx)
		}

		if b.config.SyncCommands {
			if err := b.SyncCommands(ctx); err != nil {
				b.config.Logger.Warn("failed to sync commands", "error", err)
			}
		}

		b.config.Logger.Info("bot started", "id", self.ID, "username", self.Username)

		return b.gaps.Run(ctx, b.api, self.ID, updates.AuthOptions{
			OnStart: func(ctx context.Context) {
				b.config.Logger.Info("listening for updates")
			},
		})
	})
}

// API returns the raw tg.Client for advanced operations.
func (b *Bot) API() *tg.Client {
	return b.api
}

// SelfID returns the bot's user ID.
func (b *Bot) SelfID() int64 {
	return b.selfID
}

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
	// Handle commands like /cmd@botname
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

func (f *CallbackFilter) matches(ctx *CallbackContext) bool {
	if f.DataPrefix != "" && !strings.HasPrefix(ctx.data, f.DataPrefix) {
		return false
	}

	if len(f.Users) > 0 && !slices.Contains(f.Users, ctx.userID) {
		return false
	}

	if f.Custom != nil && !f.Custom(ctx) {
		return false
	}

	return true
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

func (f *DeleteFilter) matches(ctx *DeleteContext) bool {
	if len(f.Chats) > 0 {
		targetID := ctx.channelID
		if targetID == 0 {
			targetID = ctx.chatID
		}
		if !slices.Contains(f.Chats, targetID) {
			return false
		}
	}

	if f.Custom != nil && !f.Custom(ctx) {
		return false
	}

	return true
}
