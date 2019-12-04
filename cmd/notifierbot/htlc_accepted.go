package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/tidwall/buntdb"
	"github.com/tidwall/gjson"
)

var continueHTLC = map[string]interface{}{"result": "continue"}
var failHTLC = map[string]interface{}{"result": "fail", "failure_code": 16399}

/* when we accept an HTLC it can be either
1. new -- in which case we must do
  a. check the next channel and see if it comes from an awaitable invoice;
  b. notify the user his payment has arrived;
  c. either
    i  wait for the user to say he is online and continue; or
    ii fail after x minutes.
2. old, still unprocessed -- in which case we must do
  a. check if we have attempted an outgoing payment for that hash,
    i  if not, then continue with flow 1;
    ii if yes and it's
      resolved: resolve the HTLC with the given preimage (otherwise we'll lose money);
      failed: check if it's more than 5 minutes, then we can sure it isn't going to be tried again,
              so fail the HTLC, otherwise treat as pending;
      pending: payment is still in flight, so we must keep waiting undefinitely
               (if our attempt params are correct we'll either get a permanent failure or a
                success on our payment attempt _before_ the HTLC we're holding expires).
*/
func htlc_accepted(p *plugin.Plugin, params plugin.Params) (resp interface{}) {
	scid, msatoshi, ok := getOnionData(params["onion"])
	if !ok {
		return continueHTLC
	}
	holdForMinutes := p.Args.Get("notifierbot-minutes").Int()
	extraFeePerMillionth := p.Args.Get("notifierbot-extra-fee-per-millionth").Int()

	p.Log("waiting for the bot API to be ok")
	for {
		if bot == nil {
			time.Sleep(1 * time.Second)
		} else {
			break
		}
	}

	htlc := params["htlc"].(map[string]interface{})
	hash := htlc["payment_hash"].(string)
	timeLeft := int(htlc["cltv_expiry_relative"].(float64))
	p.Logf("htlc_accepted %dmsat next=%s hash=%s timeleft=%d", msatoshi, scid, hash, timeLeft)

	// data used to notify the user and then pay his original invoice
	var telegramId int64
	var originalbolt11 string
	sendingPayment := false

	// is this an HTLC that's being retried? we should check if we've paid it to someone else
	// already so we can claim it and don't lose money
	// ~
	// actually instead of checking once at the beginning let's rerun this entire flow every x minutes
	// it will be as if lightningd was restarted every x minutes and called us again and again
	wakes, _ := awaken.Sub(context.TODO(), 1)
	defer awaken.Unsub(wakes)
	pays, _ := paid.Sub(context.TODO(), 1)
	defer paid.Unsub(pays)
	fails, _ := failed.Sub(context.TODO(), 1)
	defer failed.Unsub(fails)

	for {
		// we assign this here to not restart the counter every time the `select` below matches
		afterSomeMinutes := time.After(time.Duration(holdForMinutes) * time.Minute)
		// but it's still restarted on every loop

		sendpays, err := p.Client.CallNamed("listsendpays", "payment_hash", hash)
		if err != nil {
			p.Log("Unexpectedly failed to call listsendpays on htlc_accepted: " + err.Error())
			return continueHTLC
		}

		sendpay := sendpays.Get("payments.0")
		if sendpay.Exists() {
			// this payment was tried / it's been tried still
			p.Logf("payment was tried hash=%s status=%s", hash, sendpay.Get("status").String())

			switch sendpay.Get("status").String() {
			case "pending":
				// we'll jump to the listen flow, but skip paying and
				// notifying the user as that was already done
				sendingPayment = true
				goto listen
			case "failed":
				if !time.Unix(sendpay.Get("created_at").Int(), 0).Before(
					time.Now().Add(-(time.Minute * 15)),
				) {
					// even in "failed" case this might still be pending if it's recent enough
					sendingPayment = true
					goto listen
				}

				// otherwise we'll continue as this is probably a new payment attempt for the
				// same hash because the first one failed for some external reason.
				sendingPayment = false
				goto querynotify
			case "complete":
				// we've paid this already so let's resolve the HTLC
				return map[string]interface{}{
					"result":      "resolve",
					"payment_key": sendpay.Get("payment_preimage").String(),
				}
			}
		}

	querynotify:
		// get the telegram user for this awaitable invoice
		p.Log("querying telegram user and original invoice")
		err = db.View(func(tx *buntdb.Tx) error {
			val, err := tx.Get(hash)
			if err != nil {
				return err
			}

			data := gjson.Parse(val)
			telegramId = data.Get("telegram").Int()
			originalbolt11 = data.Get("originalbolt11").String()
			return nil
		})
		if err != nil {
			p.Logf("failed to query telegram user: %s", err.Error())
			return continueHTLC
		}
		if telegramId == 0 {
			// didn't find a telegram peer
			p.Log("didn't find a telegram peer for HTLC, continuing")
			return continueHTLC
		}

		// send telegram message
		p.Logf("sending incoming htlc message to telegram user %d, hash %s, %dmsat", telegramId, hash, msatoshi)
		err = notify(telegramId, hash, msatoshi)
		if err != nil {
			// failed to notify, so fail
			return failHTLC
		}

	listen:
		// now we wait until peer is connected to release the HTLC or give up after x min
		select {
		case <-afterSomeMinutes:
			if sendingPayment {
				// never stop here while we're still sending a payment
				continue
			}

			p.Logf("%dmin timeout for HTLC %s. failing.", holdForMinutes, hash)
			return failHTLC
		case tgid := <-wakes:
			// peer is awaken, so release the payment or do the preimage-fetching gimmick
			if int64(tgid.(int)) == telegramId {
				if sendingPayment {
					// don't attempt to send twice
					continue
				}

				p.Logf("peer %d is online. proceeding with HTLC %s.", telegramId, hash)
				if timeLeft < int(holdForMinutes+2) {
					// too risky, but this should never happen and should always be 288 anyway
					return failHTLC
				}

				p.Logf("sending payment to %s to fetch preimage for %s, timeleft=%d",
					originalbolt11, hash, timeLeft)
				sendingPayment = true
				go func() {
					ok, _, tries, err := p.Client.PayAndWaitUntilResolution(
						originalbolt11,
						map[string]interface{}{
							"exemptfee":     1,
							"maxfeepercent": extraFeePerMillionth * 1000 / 1000000,
							"maxdelaytotal": timeLeft - 23,
						})

					if err != nil {
						p.Logf("error sending preimage-fetching payment: %s", err)
						failed.TryPub(hash)
					}
					if ok == false {
						p.Logf("failed to send preimage-fetching payment: %v", tries)
						failed.TryPub(hash)
					}

					// we only handle failure here.
					// in case of success we should see a sendpay_success notification.
				}()

				// after starting a payment we don't have start the cycle again, we just keep listening
				goto listen
			}
		case fhash := <-fails:
			// see if this hash matches ours and fail if yes
			if fhash.(string) == hash {
				p.Logf("%d will not be able to receive HTLC %s. failing.", telegramId, hash)
				return failHTLC
			}

			// after getting a failed hash that doesn't belong here we simply keep listening
			goto listen
		case preimage := <-pays:
			// see if this preimage matches our hash and resolve if yes
			if bpreimage, err := hex.DecodeString(preimage.(string)); err == nil {
				if bhash := sha256.Sum256(bpreimage); hex.EncodeToString(bhash[:]) == hash {
					p.Logf("got preimage for HTLC %s from user %d. resolving.", hash, telegramId)
					return map[string]interface{}{
						"result":      "resolve",
						"payment_key": preimage,
					}
				}
			}

			// after getting a success preimage that doesn't belong here we simply keep listening
			goto listen
		}
	}

	return failHTLC

}
