package tgbot

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestParseScopeKey(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		wantScope    CommandScope
		wantLangCode string
	}{
		{
			name:         "default scope empty lang",
			key:          "default|",
			wantScope:    ScopeDefault{},
			wantLangCode: "",
		},
		{
			name:         "default scope with lang",
			key:          "default|ru",
			wantScope:    ScopeDefault{},
			wantLangCode: "ru",
		},
		{
			name:         "users scope",
			key:          "users|",
			wantScope:    ScopeAllPrivate{},
			wantLangCode: "",
		},
		{
			name:         "chats scope",
			key:          "chats|en",
			wantScope:    ScopeAllGroups{},
			wantLangCode: "en",
		},
		{
			name:         "chat_admins scope (all)",
			key:          "chat_admins|",
			wantScope:    ScopeAllGroupAdmins{},
			wantLangCode: "",
		},
		{
			name:         "chat scope with id",
			key:          "chat:123456|",
			wantScope:    ScopeChat{ChatID: 123456},
			wantLangCode: "",
		},
		{
			name:         "channel scope with id and access hash",
			key:          "channel:789012:123456|de",
			wantScope:    ScopeChannel{ChannelID: 789012, AccessHash: 123456},
			wantLangCode: "de",
		},
		{
			name:         "channel scope with username",
			key:          "channel:@mychannel|",
			wantScope:    ScopeChannelUsername{Username: "mychannel"},
			wantLangCode: "",
		},
		{
			name:         "chat_admins scope with id",
			key:          "chat_admins:123456|",
			wantScope:    ScopeChatAdmins{ChatID: 123456},
			wantLangCode: "",
		},
		{
			name:         "chat_member scope",
			key:          "chat_member:123:456:789|",
			wantScope:    ScopeChatMember{ChatID: 123, UserID: 456, UserAccessHash: 789},
			wantLangCode: "",
		},
		{
			name:         "user scope with id and access hash",
			key:          "user:123456:654321|",
			wantScope:    ScopeUser{UserID: 123456, AccessHash: 654321},
			wantLangCode: "",
		},
		{
			name:         "user scope with username",
			key:          "user:@johndoe|fr",
			wantScope:    ScopeUsername{Username: "johndoe"},
			wantLangCode: "fr",
		},
		{
			name:         "invalid key no separator",
			key:          "default",
			wantScope:    nil,
			wantLangCode: "",
		},
		{
			name:         "invalid key unknown scope",
			key:          "unknown|",
			wantScope:    nil,
			wantLangCode: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotScope, gotLangCode := parseScopeKey(tt.key)

			if gotLangCode != tt.wantLangCode {
				t.Errorf("parseScopeKey() langCode = %q, want %q", gotLangCode, tt.wantLangCode)
			}

			if tt.wantScope == nil {
				if gotScope != nil {
					t.Errorf("parseScopeKey() scope = %v, want nil", gotScope)
				}
				return
			}

			if gotScope == nil {
				t.Errorf("parseScopeKey() scope = nil, want %v", tt.wantScope)
				return
			}

			// Compare scope types and values
			switch want := tt.wantScope.(type) {
			case ScopeDefault:
				if _, ok := gotScope.(ScopeDefault); !ok {
					t.Errorf("parseScopeKey() scope type = %T, want ScopeDefault", gotScope)
				}
			case ScopeAllPrivate:
				if _, ok := gotScope.(ScopeAllPrivate); !ok {
					t.Errorf("parseScopeKey() scope type = %T, want ScopeAllPrivate", gotScope)
				}
			case ScopeAllGroups:
				if _, ok := gotScope.(ScopeAllGroups); !ok {
					t.Errorf("parseScopeKey() scope type = %T, want ScopeAllGroups", gotScope)
				}
			case ScopeAllGroupAdmins:
				if _, ok := gotScope.(ScopeAllGroupAdmins); !ok {
					t.Errorf("parseScopeKey() scope type = %T, want ScopeAllGroupAdmins", gotScope)
				}
			case ScopeChat:
				got, ok := gotScope.(ScopeChat)
				if !ok {
					t.Errorf("parseScopeKey() scope type = %T, want ScopeChat", gotScope)
				} else if got.ChatID != want.ChatID {
					t.Errorf("parseScopeKey() ChatID = %d, want %d", got.ChatID, want.ChatID)
				}
			case ScopeChannel:
				got, ok := gotScope.(ScopeChannel)
				if !ok {
					t.Errorf("parseScopeKey() scope type = %T, want ScopeChannel", gotScope)
				} else {
					if got.ChannelID != want.ChannelID {
						t.Errorf("parseScopeKey() ChannelID = %d, want %d", got.ChannelID, want.ChannelID)
					}
					if got.AccessHash != want.AccessHash {
						t.Errorf("parseScopeKey() AccessHash = %d, want %d", got.AccessHash, want.AccessHash)
					}
				}
			case ScopeChannelUsername:
				got, ok := gotScope.(ScopeChannelUsername)
				if !ok {
					t.Errorf("parseScopeKey() scope type = %T, want ScopeChannelUsername", gotScope)
				} else if got.Username != want.Username {
					t.Errorf("parseScopeKey() Username = %q, want %q", got.Username, want.Username)
				}
			case ScopeChatAdmins:
				got, ok := gotScope.(ScopeChatAdmins)
				if !ok {
					t.Errorf("parseScopeKey() scope type = %T, want ScopeChatAdmins", gotScope)
				} else if got.ChatID != want.ChatID {
					t.Errorf("parseScopeKey() ChatID = %d, want %d", got.ChatID, want.ChatID)
				}
			case ScopeChatMember:
				got, ok := gotScope.(ScopeChatMember)
				if !ok {
					t.Errorf("parseScopeKey() scope type = %T, want ScopeChatMember", gotScope)
				} else {
					if got.ChatID != want.ChatID {
						t.Errorf("parseScopeKey() ChatID = %d, want %d", got.ChatID, want.ChatID)
					}
					if got.UserID != want.UserID {
						t.Errorf("parseScopeKey() UserID = %d, want %d", got.UserID, want.UserID)
					}
					if got.UserAccessHash != want.UserAccessHash {
						t.Errorf("parseScopeKey() UserAccessHash = %d, want %d", got.UserAccessHash, want.UserAccessHash)
					}
				}
			case ScopeUser:
				got, ok := gotScope.(ScopeUser)
				if !ok {
					t.Errorf("parseScopeKey() scope type = %T, want ScopeUser", gotScope)
				} else {
					if got.UserID != want.UserID {
						t.Errorf("parseScopeKey() UserID = %d, want %d", got.UserID, want.UserID)
					}
					if got.AccessHash != want.AccessHash {
						t.Errorf("parseScopeKey() AccessHash = %d, want %d", got.AccessHash, want.AccessHash)
					}
				}
			case ScopeUsername:
				got, ok := gotScope.(ScopeUsername)
				if !ok {
					t.Errorf("parseScopeKey() scope type = %T, want ScopeUsername", gotScope)
				} else if got.Username != want.Username {
					t.Errorf("parseScopeKey() Username = %q, want %q", got.Username, want.Username)
				}
			default:
				t.Errorf("unexpected scope type: %T", tt.wantScope)
			}
		})
	}
}

func TestScopeKeyRoundtrip(t *testing.T) {
	tests := []struct {
		name     string
		scope    CommandScope
		langCode string
	}{
		{"default", ScopeDefault{}, ""},
		{"default with lang", ScopeDefault{}, "en"},
		{"users", ScopeAllPrivate{}, ""},
		{"chats", ScopeAllGroups{}, "ru"},
		{"chat_admins all", ScopeAllGroupAdmins{}, ""},
		{"chat", ScopeChat{ChatID: 123}, ""},
		{"channel", ScopeChannel{ChannelID: 456, AccessHash: 789}, "de"},
		{"channel username", ScopeChannelUsername{Username: "test"}, ""},
		{"chat_admins specific", ScopeChatAdmins{ChatID: 789}, ""},
		{"channel_admins", ScopeChannelAdmins{ChannelID: 555, AccessHash: 666}, ""},
		{"chat_member", ScopeChatMember{ChatID: 111, UserID: 222, UserAccessHash: 333}, "fr"},
		{"chat_member_channel", ScopeChatMemberChannel{ChannelID: 100, ChannelAccessHash: 101, UserID: 200, UserAccessHash: 201}, ""},
		{"user", ScopeUser{UserID: 333, AccessHash: 444}, ""},
		{"username", ScopeUsername{Username: "john"}, "es"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := scopeKeyString(tt.scope, tt.langCode)
			gotScope, gotLangCode := parseScopeKey(key)

			if gotLangCode != tt.langCode {
				t.Errorf("roundtrip langCode = %q, want %q (key: %s)", gotLangCode, tt.langCode, key)
			}

			if gotScope == nil {
				t.Errorf("roundtrip scope = nil for key %q", key)
				return
			}

			// Verify the scope type matches
			switch tt.scope.(type) {
			case ScopeDefault:
				if _, ok := gotScope.(ScopeDefault); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeDefault", gotScope)
				}
			case ScopeAllPrivate:
				if _, ok := gotScope.(ScopeAllPrivate); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeAllPrivate", gotScope)
				}
			case ScopeAllGroups:
				if _, ok := gotScope.(ScopeAllGroups); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeAllGroups", gotScope)
				}
			case ScopeAllGroupAdmins:
				if _, ok := gotScope.(ScopeAllGroupAdmins); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeAllGroupAdmins", gotScope)
				}
			case ScopeChat:
				if _, ok := gotScope.(ScopeChat); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeChat", gotScope)
				}
			case ScopeChannel:
				if _, ok := gotScope.(ScopeChannel); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeChannel", gotScope)
				}
			case ScopeChannelUsername:
				if _, ok := gotScope.(ScopeChannelUsername); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeChannelUsername", gotScope)
				}
			case ScopeChatAdmins:
				if _, ok := gotScope.(ScopeChatAdmins); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeChatAdmins", gotScope)
				}
			case ScopeChannelAdmins:
				if _, ok := gotScope.(ScopeChannelAdmins); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeChannelAdmins", gotScope)
				}
			case ScopeChatMember:
				if _, ok := gotScope.(ScopeChatMember); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeChatMember", gotScope)
				}
			case ScopeChatMemberChannel:
				if _, ok := gotScope.(ScopeChatMemberChannel); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeChatMemberChannel", gotScope)
				}
			case ScopeUser:
				if _, ok := gotScope.(ScopeUser); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeUser", gotScope)
				}
			case ScopeUsername:
				if _, ok := gotScope.(ScopeUsername); !ok {
					t.Errorf("roundtrip scope type mismatch: got %T, want ScopeUsername", gotScope)
				}
			}
		})
	}
}

func TestCommandScopesFile(t *testing.T) {
	tmpDir := t.TempDir()

	bot := &Bot{
		config: Config{
			SessionDir: tmpDir,
		},
	}

	// Test save and load
	scopes := []string{"default|", "user:123:456|ru", "channel:@test|"}

	if err := bot.saveCommandScopes(scopes); err != nil {
		t.Fatalf("saveCommandScopes() error = %v", err)
	}

	// Verify file exists
	filePath := filepath.Join(tmpDir, "command_scopes.json")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("command_scopes.json was not created")
	}

	// Load and verify
	loaded := bot.loadCommandScopes()
	if len(loaded) != len(scopes) {
		t.Fatalf("loadCommandScopes() returned %d items, want %d", len(loaded), len(scopes))
	}

	for i, s := range scopes {
		if loaded[i] != s {
			t.Errorf("loadCommandScopes()[%d] = %q, want %q", i, loaded[i], s)
		}
	}
}

func TestLoadCommandScopesEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	bot := &Bot{
		config: Config{
			SessionDir: tmpDir,
		},
	}

	// Load from non-existent file should return nil
	loaded := bot.loadCommandScopes()
	if loaded != nil {
		t.Errorf("loadCommandScopes() = %v, want nil for non-existent file", loaded)
	}
}

func TestLoadCommandScopesInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()

	bot := &Bot{
		config: Config{
			SessionDir: tmpDir,
			Logger:     slog.Default(),
		},
	}

	// Write invalid JSON
	filePath := filepath.Join(tmpDir, "command_scopes.json")
	if err := os.WriteFile(filePath, []byte("not valid json"), 0600); err != nil {
		t.Fatal(err)
	}

	// Load should return nil for invalid JSON
	loaded := bot.loadCommandScopes()
	if loaded != nil {
		t.Errorf("loadCommandScopes() = %v, want nil for invalid JSON", loaded)
	}
}
