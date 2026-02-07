# telekit

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
go get github.com/en9inerd/telekit
```

## Usage

```go
package main

import (
	"context"
	"log"

	"github.com/en9inerd/telekit"
)

func main() {
	bot, err := telekit.New(telekit.Config{
		APIID:    12345,
		APIHash:  "your-api-hash",
		BotToken: "your-bot-token",
	})
	if err != nil {
		log.Fatal(err)
	}

	bot.Command("start", nil, func(ctx *telekit.Context) error {
		return ctx.Reply("Hello!")
	})

	bot.OnChannelPost(channelID, func(ctx *telekit.Context) error {
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
