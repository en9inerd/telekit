package tgbot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gotd/td/tg"
)

// SyncCommands registers all commands with Telegram so they appear in the bot menu.
// It resets previous command scopes and sets new ones based on registered commands.
// Should be called after all commands are registered and the bot is running.
func (b *Bot) SyncCommands(ctx context.Context) error {
	if b.api == nil {
		return ErrBotNotRunning
	}

	b.mu.RLock()
	handlers := b.commandHandlers
	b.mu.RUnlock()

	// Group commands by scope and language
	grouped := make(map[string][]tg.BotCommand)
	scopes := make(map[string]CommandScope)

	for _, h := range handlers {
		if h.description == "" {
			continue
		}

		scope := h.scope
		if scope == nil {
			scope = ScopeDefault{}
		}

		key := scopeKeyString(scope, h.langCode)
		scopes[key] = scope

		// Check for duplicate command in same scope
		exists := false
		for _, cmd := range grouped[key] {
			if cmd.Command == h.name {
				exists = true
				break
			}
		}
		if !exists {
			grouped[key] = append(grouped[key], tg.BotCommand{
				Command:     h.name,
				Description: h.description,
			})
		}
	}

	if len(grouped) == 0 {
		b.config.Logger.Debug("no commands with descriptions to sync")
		return nil
	}

	if err := b.ResetCommands(ctx); err != nil {
		b.config.Logger.Warn("failed to reset previous commands", "error", err)
	}

	var scopeKeys []string

	for key, commands := range grouped {
		scope := scopes[key]
		langCode := extractLangCode(key)

		resolvedScope, err := b.resolveScope(ctx, scope)
		if err != nil {
			b.config.Logger.Error("failed to resolve scope", "scope", key, "error", err)
			continue
		}

		_, err = b.api.BotsSetBotCommands(ctx, &tg.BotsSetBotCommandsRequest{
			Scope:    resolvedScope,
			LangCode: langCode,
			Commands: commands,
		})
		if err != nil {
			b.config.Logger.Error("failed to set commands", "scope", key, "error", err)
			continue
		}

		scopeKeys = append(scopeKeys, key)

		b.config.Logger.Debug("set commands for scope", "scope", key, "count", len(commands))
	}

	if err := b.saveCommandScopes(scopeKeys); err != nil {
		b.config.Logger.Warn("failed to save command scopes", "error", err)
	}

	b.config.Logger.Info("synced commands to Telegram", "scopes", len(grouped))
	return nil
}

// ResetCommands removes all bot commands from Telegram.
// It resets commands for all scope+langCode combinations that were previously saved.
func (b *Bot) ResetCommands(ctx context.Context) error {
	if b.api == nil {
		return ErrBotNotRunning
	}

	previousScopes := b.loadCommandScopes()

	if len(previousScopes) == 0 {
		// Fallback for first run or missing state file
		scopesToReset := []tg.BotCommandScopeClass{
			&tg.BotCommandScopeDefault{},
			&tg.BotCommandScopeUsers{},
			&tg.BotCommandScopeChats{},
			&tg.BotCommandScopeChatAdmins{},
		}

		for _, scope := range scopesToReset {
			_, err := b.api.BotsResetBotCommands(ctx, &tg.BotsResetBotCommandsRequest{
				Scope:    scope,
				LangCode: "",
			})
			if err != nil {
				b.config.Logger.Debug("failed to reset scope", "error", err)
			}
		}

		b.config.Logger.Debug("reset bot commands (fallback)")
		return nil
	}

	for _, key := range previousScopes {
		scope, langCode := parseScopeKey(key)
		if scope == nil {
			b.config.Logger.Debug("skipping invalid scope key", "key", key)
			continue
		}

		resolvedScope, err := b.resolveScope(ctx, scope)
		if err != nil {
			b.config.Logger.Debug("failed to resolve scope for reset", "key", key, "error", err)
			continue
		}

		_, err = b.api.BotsResetBotCommands(ctx, &tg.BotsResetBotCommandsRequest{
			Scope:    resolvedScope,
			LangCode: langCode,
		})
		if err != nil {
			b.config.Logger.Debug("failed to reset scope", "key", key, "error", err)
		}
	}

	b.config.Logger.Debug("reset bot commands", "count", len(previousScopes))
	return nil
}

// SetCommandsForScope sets commands for a specific scope and language.
// Username-based scopes (ScopeChannelUsername, ScopeUsername) are resolved automatically.
func (b *Bot) SetCommandsForScope(ctx context.Context, scope CommandScope, langCode string, commands []CommandRegistration) error {
	if b.api == nil {
		return ErrBotNotRunning
	}

	if scope == nil {
		scope = ScopeDefault{}
	}

	resolvedScope, err := b.resolveScope(ctx, scope)
	if err != nil {
		return fmt.Errorf("failed to resolve scope: %w", err)
	}

	var tgCommands []tg.BotCommand
	for _, cmd := range commands {
		tgCommands = append(tgCommands, tg.BotCommand{
			Command:     cmd.Name,
			Description: cmd.Description,
		})
	}

	_, err = b.api.BotsSetBotCommands(ctx, &tg.BotsSetBotCommandsRequest{
		Scope:    resolvedScope,
		LangCode: langCode,
		Commands: tgCommands,
	})
	return err
}

// ChannelInfo contains resolved channel information.
type ChannelInfo struct {
	ID         int64
	AccessHash int64
	Username   string // empty for private channels
	Title      string
}

// ResolveIdentifier resolves an identifier (numeric ID or @username) and returns ID, access hash, and display title.
// For channels, title fallback order: username || title. For users, title is empty.
func (b *Bot) ResolveIdentifier(ctx context.Context, identifier string, isChannel bool) (id, accessHash int64, title string, err error) {
	identifier = strings.TrimSpace(identifier)

	if id, err := strconv.ParseInt(identifier, 10, 64); err == nil {
		return id, 0, strconv.FormatInt(id, 10), nil
	}

	username := strings.TrimPrefix(identifier, "@")

	if isChannel {
		info, err := b.ResolveChannelInfo(ctx, username)
		if err != nil {
			return 0, 0, "", err
		}
		// Fallback: username || title
		title = info.Username
		if title == "" {
			title = info.Title
		}
		return info.ID, info.AccessHash, title, nil
	}

	userID, accessHash, err := b.ResolveUser(ctx, username)
	if err != nil {
		return 0, 0, "", err
	}
	return userID, accessHash, "", nil
}

// ResolveChannel resolves a channel by username and returns its ID and access hash.
func (b *Bot) ResolveChannel(ctx context.Context, username string) (channelID, accessHash int64, err error) {
	info, err := b.ResolveChannelInfo(ctx, username)
	if err != nil {
		return 0, 0, err
	}
	return info.ID, info.AccessHash, nil
}

// ResolveChannelInfo resolves a channel by username and returns full channel info.
func (b *Bot) ResolveChannelInfo(ctx context.Context, username string) (*ChannelInfo, error) {
	if b.api == nil {
		return nil, ErrBotNotRunning
	}

	resolved, err := b.api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
		Username: username,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to resolve @%s: %w", username, err)
	}

	for _, chat := range resolved.Chats {
		if channel, ok := chat.(*tg.Channel); ok {
			return &ChannelInfo{
				ID:         channel.ID,
				AccessHash: channel.AccessHash,
				Username:   channel.Username,
				Title:      channel.Title,
			}, nil
		}
	}
	return nil, fmt.Errorf("@%s is not a channel", username)
}

// ResolveUser resolves a user by username and returns their ID and access hash.
func (b *Bot) ResolveUser(ctx context.Context, username string) (userID, accessHash int64, err error) {
	if b.api == nil {
		return 0, 0, ErrBotNotRunning
	}

	resolved, err := b.api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
		Username: username,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to resolve @%s: %w", username, err)
	}

	for _, user := range resolved.Users {
		if u, ok := user.(*tg.User); ok {
			return u.ID, u.AccessHash, nil
		}
	}
	return 0, 0, fmt.Errorf("@%s is not a user", username)
}

func (b *Bot) commandScopesFile() string {
	return filepath.Join(b.config.SessionDir, "command_scopes.json")
}

func (b *Bot) loadCommandScopes() []string {
	data, err := os.ReadFile(b.commandScopesFile())
	if err != nil {
		return nil
	}

	var scopes []string
	if err := json.Unmarshal(data, &scopes); err != nil {
		b.config.Logger.Debug("failed to parse command scopes file", "error", err)
		return nil
	}

	return scopes
}

func (b *Bot) saveCommandScopes(scopes []string) error {
	data, err := json.Marshal(scopes)
	if err != nil {
		return err
	}

	return os.WriteFile(b.commandScopesFile(), data, 0600)
}

// resolveScope resolves scopes that need lookup (e.g., ScopeChannelUsername, ScopeUsername).
func (b *Bot) resolveScope(ctx context.Context, scope CommandScope) (tg.BotCommandScopeClass, error) {
	switch s := scope.(type) {
	case ScopeChannelUsername:
		resolved, err := b.api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
			Username: s.Username,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to resolve @%s: %w", s.Username, err)
		}

		for _, chat := range resolved.Chats {
			if channel, ok := chat.(*tg.Channel); ok {
				return &tg.BotCommandScopePeer{
					Peer: &tg.InputPeerChannel{
						ChannelID:  channel.ID,
						AccessHash: channel.AccessHash,
					},
				}, nil
			}
		}
		return nil, fmt.Errorf("@%s is not a channel", s.Username)

	case ScopeUsername:
		resolved, err := b.api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
			Username: s.Username,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to resolve @%s: %w", s.Username, err)
		}

		for _, user := range resolved.Users {
			if u, ok := user.(*tg.User); ok {
				return &tg.BotCommandScopePeer{
					Peer: &tg.InputPeerUser{
						UserID:     u.ID,
						AccessHash: u.AccessHash,
					},
				}, nil
			}
		}
		return nil, fmt.Errorf("@%s is not a user", s.Username)

	default:
		return scope.toTG(), nil
	}
}

func scopeKeyString(scope CommandScope, langCode string) string {
	if scope == nil {
		scope = ScopeDefault{}
	}

	var scopeName string
	switch s := scope.(type) {
	case ScopeDefault:
		scopeName = "default"
	case ScopeAllPrivate:
		scopeName = "users"
	case ScopeAllGroups:
		scopeName = "chats"
	case ScopeAllGroupAdmins:
		scopeName = "chat_admins"
	case ScopeChat:
		scopeName = fmt.Sprintf("chat:%d", s.ChatID)
	case ScopeChannel:
		scopeName = fmt.Sprintf("channel:%d:%d", s.ChannelID, s.AccessHash)
	case ScopeChatAdmins:
		scopeName = fmt.Sprintf("chat_admins:%d", s.ChatID)
	case ScopeChannelAdmins:
		scopeName = fmt.Sprintf("channel_admins:%d:%d", s.ChannelID, s.AccessHash)
	case ScopeChatMember:
		scopeName = fmt.Sprintf("chat_member:%d:%d:%d", s.ChatID, s.UserID, s.UserAccessHash)
	case ScopeChatMemberChannel:
		scopeName = fmt.Sprintf("channel_member:%d:%d:%d:%d", s.ChannelID, s.ChannelAccessHash, s.UserID, s.UserAccessHash)
	case ScopeUser:
		scopeName = fmt.Sprintf("user:%d:%d", s.UserID, s.AccessHash)
	case ScopeUsername:
		scopeName = fmt.Sprintf("user:@%s", s.Username)
	case ScopeChannelUsername:
		scopeName = fmt.Sprintf("channel:@%s", s.Username)
	default:
		scopeName = "unknown"
	}

	return scopeName + "|" + langCode
}

func extractLangCode(key string) string {
	if i := strings.LastIndexByte(key, '|'); i >= 0 {
		return key[i+1:]
	}
	return ""
}

func parseScopeKey(key string) (CommandScope, string) {
	parts := strings.SplitN(key, "|", 2)
	if len(parts) != 2 {
		return nil, ""
	}

	scopeStr := parts[0]
	langCode := parts[1]

	switch {
	case scopeStr == "default":
		return ScopeDefault{}, langCode
	case scopeStr == "users":
		return ScopeAllPrivate{}, langCode
	case scopeStr == "chats":
		return ScopeAllGroups{}, langCode
	case scopeStr == "chat_admins":
		return ScopeAllGroupAdmins{}, langCode
	case strings.HasPrefix(scopeStr, "chat:"):
		var chatID int64
		if n, _ := fmt.Sscanf(scopeStr, "chat:%d", &chatID); n != 1 {
			return nil, ""
		}
		return ScopeChat{ChatID: chatID}, langCode
	case strings.HasPrefix(scopeStr, "channel_member:"):
		var channelID, channelAccessHash, userID, userAccessHash int64
		if n, _ := fmt.Sscanf(scopeStr, "channel_member:%d:%d:%d:%d", &channelID, &channelAccessHash, &userID, &userAccessHash); n != 4 {
			return nil, ""
		}
		return ScopeChatMemberChannel{
			ChannelID:         channelID,
			ChannelAccessHash: channelAccessHash,
			UserID:            userID,
			UserAccessHash:    userAccessHash,
		}, langCode
	case strings.HasPrefix(scopeStr, "channel:"):
		if strings.HasPrefix(scopeStr, "channel:@") {
			var username string
			if n, _ := fmt.Sscanf(scopeStr, "channel:@%s", &username); n != 1 {
				return nil, ""
			}
			return ScopeChannelUsername{Username: username}, langCode
		}
		var channelID, accessHash int64
		if n, _ := fmt.Sscanf(scopeStr, "channel:%d:%d", &channelID, &accessHash); n != 2 {
			return nil, ""
		}
		return ScopeChannel{ChannelID: channelID, AccessHash: accessHash}, langCode
	case strings.HasPrefix(scopeStr, "chat_admins:"):
		var chatID int64
		if n, _ := fmt.Sscanf(scopeStr, "chat_admins:%d", &chatID); n != 1 {
			return nil, ""
		}
		return ScopeChatAdmins{ChatID: chatID}, langCode
	case strings.HasPrefix(scopeStr, "channel_admins:"):
		var channelID, accessHash int64
		if n, _ := fmt.Sscanf(scopeStr, "channel_admins:%d:%d", &channelID, &accessHash); n != 2 {
			return nil, ""
		}
		return ScopeChannelAdmins{ChannelID: channelID, AccessHash: accessHash}, langCode
	case strings.HasPrefix(scopeStr, "chat_member:"):
		var chatID, userID, userAccessHash int64
		if n, _ := fmt.Sscanf(scopeStr, "chat_member:%d:%d:%d", &chatID, &userID, &userAccessHash); n != 3 {
			return nil, ""
		}
		return ScopeChatMember{ChatID: chatID, UserID: userID, UserAccessHash: userAccessHash}, langCode
	case strings.HasPrefix(scopeStr, "user:"):
		if strings.HasPrefix(scopeStr, "user:@") {
			var username string
			if n, _ := fmt.Sscanf(scopeStr, "user:@%s", &username); n != 1 {
				return nil, ""
			}
			return ScopeUsername{Username: username}, langCode
		}
		var userID, accessHash int64
		if n, _ := fmt.Sscanf(scopeStr, "user:%d:%d", &userID, &accessHash); n != 2 {
			return nil, ""
		}
		return ScopeUser{UserID: userID, AccessHash: accessHash}, langCode
	default:
		return nil, ""
	}
}
