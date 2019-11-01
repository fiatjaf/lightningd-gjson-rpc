package main

import (
	"encoding/hex"
	"strconv"
	"time"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/tidwall/buntdb"
)

func handle(p *plugin.Plugin, upd tgbotapi.Update) {
	if upd.Message != nil {
		p.Logf("bot received message: %s", upd.Message.Text)
		handleMessage(p, upd.Message)
	} else if upd.CallbackQuery != nil {
		p.Logf("bot received click: %s", upd.CallbackQuery.Data)
		answer := handleCallbackQuery(p, upd.CallbackQuery)
		bot.AnswerCallbackQuery(tgbotapi.CallbackConfig{
			CallbackQueryID: upd.CallbackQuery.ID,
			Text:            answer,
		})
	}
}

func notify(telegramId int64, hash string, msatoshi int) error {
	_, err := bot.Send(tgbotapi.MessageConfig{
		BaseChat: tgbotapi.BaseChat{
			ChatID: telegramId,
			ReplyMarkup: &tgbotapi.InlineKeyboardMarkup{
				InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
					[]tgbotapi.InlineKeyboardButton{
						tgbotapi.NewInlineKeyboardButtonData("Ok, its on!", "connected"),
						tgbotapi.NewInlineKeyboardButtonData("No, fail that payment", "fail"),
					},
				},
			},
		},
		Text: `<code>` + hash + `</code>

A <i>` + strconv.Itoa(int(msatoshi/1000)) + `</i> payment has arrived for you. Turn on your wallet to receive. You have 30 minutes.`,
		ParseMode: "HTML",
	})
	return err
}

func handleMessage(p *plugin.Plugin, message *tgbotapi.Message) {
	var telegramId = message.From.ID

	if bolt11, ok := searchForInvoice(message); ok {
		// it's an invoice we must replace
		hash, newExpiry, newInvoice, err := makeInvoice(p, bolt11)
		if err != nil {
			bot.Send(tgbotapi.MessageConfig{
				BaseChat:  tgbotapi.BaseChat{ChatID: message.Chat.ID},
				Text:      "Error translating invoice: " + err.Error(),
				ParseMode: "HTML",
			})
			return
		}

		bot.Send(tgbotapi.MessageConfig{
			BaseChat:  tgbotapi.BaseChat{ChatID: message.Chat.ID, ReplyToMessageID: message.MessageID},
			Text:      "Awaitable BOLT11 invoice: <code>" + newInvoice + "</code>",
			ParseMode: "HTML",
		})
		if err != nil {
			return
		}

		db.Update(func(tx *buntdb.Tx) error {
			tx.Set(hash, `{"telegram": `+strconv.Itoa(telegramId)+`, "originalbolt11": "`+bolt11+`"}`,
				&buntdb.SetOptions{Expires: true, TTL: time.Duration(newExpiry) * time.Second * 2})
			return nil
		})
	} else if b, err := hex.DecodeString(message.Text); err == nil && len(b) == 33 {
		// it's a node id we must associate with this account
		nodeid := message.Text

		// check if node has an account with us
		peers, err := p.Client.Call("listpeers", nodeid)
		if err != nil || !peers.Get("peers.0").Exists() {
			bot.Send(tgbotapi.MessageConfig{
				BaseChat:  tgbotapi.BaseChat{ChatID: message.Chat.ID},
				Text:      "You don't have a channel with us.",
				ParseMode: "HTML",
			})
			return
		}

		db.Update(func(tx *buntdb.Tx) error {
			tx.Set(nodeid, `{"telegram": `+strconv.Itoa(telegramId)+`}`, nil)
			return nil
		})

		bot.Send(tgbotapi.MessageConfig{
			BaseChat:  tgbotapi.BaseChat{ChatID: message.Chat.ID},
			Text:      "Done.",
			ParseMode: "HTML",
		})
	} else {
		// show status if exists
		var pre string
		db.View(func(tx *buntdb.Tx) error {
			nodeid, err := tx.Get(strconv.Itoa(telegramId))
			if err != nil {
				return err
			}
			pre = "You're connected with the node id <code>" + nodeid + "</code>.\n\n"
			return nil
		})

		// show instructions
		bot.Send(tgbotapi.MessageConfig{
			BaseChat:  tgbotapi.BaseChat{ChatID: message.Chat.ID},
			Text:      pre + "Send your node id if you're connected to us at <code>" + info.Get("id").String() + "</code> or any BOLT11 invoice to translate if not.",
			ParseMode: "HTML",
		})
	}
}

func handleCallbackQuery(p *plugin.Plugin, cb *tgbotapi.CallbackQuery) (answer string) {
	var telegramId = cb.From.ID

	// remove keyboard (always)
	defer bot.Send(tgbotapi.EditMessageReplyMarkupConfig{
		BaseEdit: tgbotapi.BaseEdit{
			MessageID: cb.Message.MessageID,
			ChatID:    cb.Message.Chat.ID,
			ReplyMarkup: &tgbotapi.InlineKeyboardMarkup{
				InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
					[]tgbotapi.InlineKeyboardButton{},
				},
			},
		},
	})

	switch cb.Data {
	case "connected":
		awaken.Pub(telegramId)
		return "Receiving payment..."
	case "fail":
		hash := cb.Message.Text[0:64]
		failed.Pub(hash)
		return "Payment rejected!"
	}

	return ""
}
