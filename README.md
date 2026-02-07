# tgbot

A high-level framework for building Telegram bots using the MTProto protocol via [gotd/td](https://github.com/gotd/td).

## Features

- Event handling with filters (channels, users, message types)
- Command parsing with typed parameter validation
- Album (grouped media) handling
- Command menu sync with scoped visibility
- Bot profile management
- Session management

## Installation

```bash
go get github.com/en9inerd/tgbot
```

## Usage

```go
package main

import (
	"context"
	"log"

	"github.com/en9inerd/tgbot"
)

func main() {
	bot, err := tgbot.New(tgbot.Config{
		APIID:    12345,
		APIHash:  "your-api-hash",
		BotToken: "your-bot-token",
	})
	if err != nil {
		log.Fatal(err)
	}

	bot.Command("start", nil, func(ctx *tgbot.Context) error {
		return ctx.Reply("Hello!")
	})

	bot.OnChannelPost(channelID, func(ctx *tgbot.Context) error {
		// Handle new channel post
		return nil
	})

	if err := bot.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
```

## License

MIT
