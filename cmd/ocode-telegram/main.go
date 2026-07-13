// Command ocode-telegram is a local companion bot that lets you drive every
// running ocode instance (those with /rc enabled) from Telegram.
//
// It discovers instances via the local registry written by ocode's /rc command
// and relays your Telegram messages to the selected instance's web/RC server,
// streaming the agent's response back. No public ingress is required: the bot
// runs on the same machine as ocode and only Telegram is external.
//
// Environment:
//
//	OCODE_TG_TOKEN        (required) Telegram bot token from @BotFather
//	OCODE_TG_ALLOWED_USERS (optional) comma-separated Telegram user ids; if set,
//	                      only those users may use the bot
//	OCODE_TG_RC_DIR       (optional) override the instance registry directory
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/u007/ocode/internal/telegram"
)

func main() {
	token := os.Getenv("OCODE_TG_TOKEN")
	if token == "" {
		log.Fatal("OCODE_TG_TOKEN is required (create a bot via @BotFather)")
	}

	var allowed []int64
	if v := os.Getenv("OCODE_TG_ALLOWED_USERS"); v != "" {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			id, err := strconv.ParseInt(p, 10, 64)
			if err != nil {
				log.Fatalf("invalid OCODE_TG_ALLOWED_USERS entry %q: %v", p, err)
			}
			allowed = append(allowed, id)
		}
	}

	rcDir := os.Getenv("OCODE_TG_RC_DIR")

	bot := telegram.NewBot(token, allowed, rcDir)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Print("ocode-telegram: polling Telegram (Ctrl-C to stop)")
	if err := bot.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("bot stopped: %v", err)
	}
	log.Print("ocode-telegram: stopped")
}
