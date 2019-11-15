package main

import (
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
		bot.Send(tgbotapi.MessageConfig{
			BaseChat:  tgbotapi.BaseChat{ChatID: message.Chat.ID, ReplyToMessageID: message.MessageID},
			Text:      "Translating into an awaitable invoice...",
			ParseMode: "HTML",
		})

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
			Text:      "<pre>" + newInvoice + "</pre>",
			ParseMode: "HTML",
		})

		db.Update(func(tx *buntdb.Tx) error {
			tx.Set(hash, `{"telegram": `+strconv.Itoa(telegramId)+`, "originalbolt11": "`+bolt11+`"}`,
				&buntdb.SetOptions{Expires: true, TTL: time.Duration(newExpiry) * time.Second * 2})
			return nil
		})
	} else {
		// show instructions
		bot.Send(tgbotapi.MessageConfig{
			BaseChat: tgbotapi.BaseChat{ChatID: message.Chat.ID},
			Text: `
1. You have a mobile wallet and want to receive a Lightning payment.
2. Generate an invoice and <b>paste it here</b> to get a corresponding <i>awaitable invoice</i>.
3. Give the awaitable invoice to the payer.
4. When they pay you'll get a notification and will have <i>30 minutes</i> to open your wallet.
            `,
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
