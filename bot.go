package telekit

import (
	"context"
	"os"
	"path/filepath"
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
			floodWaitMiddleware{},
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
