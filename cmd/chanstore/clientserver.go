package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/coreos/bbolt"
	lightning "github.com/fiatjaf/lightningd-gjson-rpc"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/lucsky/cuid"
)

const (
	REPLY_INVOICE        = 63241
	REPLY_CHANNEL        = 63243
	MSG_REQUEST_INVOICE  = 63245
	MSG_ADD_CHANNEL      = 63247
	MSG_REPORT_CHANNEL   = 63249
	MSG_REQUEST_CHANNELS = 63251

	PROBE_AMOUNT = 100000
)

func custommsg(p *plugin.Plugin, payload plugin.Params) (resp interface{}) {
	peer := payload.Get("peer_id").String()
	message := payload.Get("message").String()
	resp = continuehook

	code, err := strconv.ParseInt(message[:4], 16, 64)
	if err != nil {
		p.Logf("got invalid custommsg: %s (%s)", message, err.Error())
		return
	}

	switch code {
	case MSG_REQUEST_INVOICE:
		// peer wants to add a channel to our database, send an invoice
		res, err := p.Client.Call("invoice", p.Args.Get("chanstore-price").String()+"sat", "chanstore/"+peer, "Ticket to include a channel on chanstore.")
		if err != nil {
			p.Logf("error creating invoice: %s", err.Error())
			return
		}
		bolt11 := res.Get("bolt11").String()

		_, err = p.Client.Call("dev-sendcustommsg", peer,
			strconv.FormatInt(REPLY_INVOICE, 16)+
				hex.EncodeToString([]byte(bolt11)))
		if err != nil {
			p.Logf("error sending reply: %s", err.Error())
			return
		}
	case MSG_ADD_CHANNEL:
		// receive a channel to be added to the database
		var halfchannels []lightning.Channel
		data, _ := hex.DecodeString(message[4:])
		err := json.Unmarshal(data, &halfchannels)
		if err != nil {
			p.Logf("invalid channel-add message: %s", err.Error())
			return
		}

		if err := halfchannelsAreOk(halfchannels); err != nil {
			p.Log(err.Error())
			return
		}

		// check if invoice has been paid
		res, _ := p.Client.Call("listinvoices", "chanstore/"+peer)
		if res.Get("invoices.0.status").String() != "paid" {
			p.Logf("channel-add, but chanstore/%s is not paid", peer)
			return
		}

		// delete invoice so the user can request a new one later
		p.Client.Call("delinvoice", "chanstore/"+peer, "paid")

		p.Log("adding channels: ", halfchannels)

		// check if channels really exist and can route
		// by sending a circular payment to us passing through them
		for _, channel := range halfchannels {
			res, _ = p.Client.Call("invoice", PROBE_AMOUNT,
				cuid.Slug(), "chanstore probe")
			bolt11, _ := p.Client.Call("decodepay",
				res.Get("bolt11").String())
			hash := bolt11.Get("payment_hash").String()
			we := bolt11.Get("payee").String()
			cltv := bolt11.Get("min_final_cltv_expiry").Int()
			exclude := make([]string, 0, 20)

			for i := 0; i < 30; i++ {
				path1, err := p.Client.GetPath(
					channel.Source, PROBE_AMOUNT, we, exclude, 20, 0.5)
				if err != nil {
					p.Logf("channel-add route failure: %s", err.Error())
					return
				}
				path2, err := p.Client.GetPath(
					we, PROBE_AMOUNT, channel.Destination, exclude, 20, 0.5)
				if err != nil {
					p.Logf("channel-add route failure: %s", err.Error())
					return
				}
				path := append(path1, &channel)
				path = append(path, path2...)
				route := lightning.PathToRoute(path, PROBE_AMOUNT, cltv, 0, 0)

				// naïve maxfeepercent
				if route[0].Msatoshi > PROBE_AMOUNT*1.01 {
					continue
				}

				p.Client.Call("sendpay", route, hash)
				_, err = p.Client.Call("waitsendpay", hash)
				if err != nil {
					if errc, ok := err.(lightning.ErrorCommand); ok {
						d, ok := errc.Data.(map[string]interface{})
						if !ok {
							p.Logf("probe payment failed: %s", err.Error())
							return
						}

						if _, ok := d["erring_direction"]; !ok {
							p.Logf("probe payment failed: %s", err.Error())
							return
						}

						// naïvely exclude erring channel
						erringChannel := fmt.Sprintf("%s/%d",
							d["erring_channel"], d["erring_direction"])
						exclude = append(exclude, erringChannel)
					} else {
						p.Logf("probe payment failed: %s", err.Error())
						return
					}
				}

				// payment went through
				break
			}
		}

		// add channel to database
		var halfchannelsdata []byte
		db.Update(func(tx *bbolt.Tx) error {
			bucket := tx.Bucket([]byte("server"))

			last, _ := bucket.Cursor().Last()
			halfchannelsdata, _ = json.Marshal(halfchannels)
			bucket.Put(last, halfchannelsdata)
			return nil
		})

		// notify all peers of this new channel
		res, err = p.Client.Call("listpeers")
		if err != nil {
			p.Logf("failed to listpeers to notify: %s", err.Error())
			return
		}
		for _, peerdata := range res.Get("peers").Array() {
			peerid := peerdata.Get("id").String()
			p.Client.Call("dev-sendcustommsg", peerid,
				strconv.FormatInt(REPLY_CHANNEL, 16)+
					hex.EncodeToString(halfchannelsdata))
		}

	case MSG_REPORT_CHANNEL:
	case MSG_REQUEST_CHANNELS:
		db.View(func(tx *bbolt.Tx) error {
			c := tx.Bucket([]byte("server")).Cursor()

			since, _ := hex.DecodeString(message[4:])
			for _, v := c.Seek(since); v != nil; _, v = c.Next() {
				_, err = p.Client.Call("dev-sendcustommsg", peer,
					strconv.FormatInt(REPLY_CHANNEL, 16)+
						hex.EncodeToString(v))
				if err != nil {
					p.Logf("error sending reply: %s", err.Error())
				}
			}
			return nil
		})
	case REPLY_CHANNEL:
		// we got a channel (as an array of two halfchannels)
		// if is from a server we trust, add it to our database
		if _, ok := serverlist[peer]; ok {
			var halfchannels []lightning.Channel
			data, _ := hex.DecodeString(message[4:])
			err := json.Unmarshal(data, &halfchannels)
			if err != nil {
				p.Logf("invalid channel-reply message: %s", err.Error())
				return
			}

			if err := halfchannelsAreOk(halfchannels); err != nil {
				p.Log(err.Error())
				return
			}

			p.Logf("got %s from %s", halfchannels[0].ShortChannelID, peer)

			// add channel to database
			db.Update(func(tx *bbolt.Tx) error {
				bucket := tx.Bucket([]byte(peer))
				data, _ := json.Marshal(halfchannels)
				bucket.Put([]byte(halfchannels[0].ShortChannelID), data)
				return nil
			})
		} else {
			p.Logf("got channel %s, but we don't know them", peer)
		}
	case REPLY_INVOICE:
		// if this is expected, pay the invoice and send the channel
		if channel, ok := channelswaitingtosend[peer]; ok {
			// it is expected (we've requested this invoice earlier)
			bbolt11, err := hex.DecodeString(message[4:])
			if err != nil {
				p.Logf("invalid invoice-reply: %s", err.Error())
				return
			}
			bolt11 := string(bbolt11)

			// check amount
			d, err := p.Client.Call("decodepay", bolt11)
			if err != nil || d.Get("msatoshi").Int() > 500000 {
				p.Logf("invoice too big: %s %s", err.Error(), bolt11)
				return
			}

			// pay it
			p.Client.Call("pay", bolt11)

			// send the channel
			jchanneldata, _ := json.Marshal(channel)
			_, err = p.Client.Call("dev-sendcustommsg", peer,
				strconv.FormatInt(MSG_ADD_CHANNEL, 16)+
					hex.EncodeToString(jchanneldata))
			if err != nil {
				p.Logf("error sending channel-add: %s", err.Error())
			}
		}
	}

	return
}

func onInit(p *plugin.Plugin) {
	// open database
	dbfile := filepath.Join(filepath.Dir(p.Client.Path), DATABASE_FILE)
	db, err = bbolt.Open(dbfile, 0644, nil)
	if err != nil {
		p.Logf("unable to open database at %s: %w", dbfile, err)
		os.Exit(1)
	}
	defer db.Close()

	// parse list of servers to connect to
	for _, server := range strings.Split(p.Args.Get("chanstore-connect").String(), ",") {
		if server == "" {
			continue
		}
		serverlist[server] = true
	}

	// create local channel bucket
	db.Update(func(tx *bbolt.Tx) error {
		// a bucket for channels people may send to us
		tx.CreateBucketIfNotExists([]byte("server"))

		// then there are other buckets, one with each peer id as name
		// for channels we may get from other servers and that we will
		// use locally
		for server, _ := range serverlist {
			tx.CreateBucketIfNotExists([]byte(server))
		}

		return nil
	})

	// connect and fetch channels from servers
	for server, _ := range serverlist {
		db.View(func(tx *bbolt.Tx) error {
			bucket := tx.Bucket([]byte(server))
			stats := bucket.Stats()

			// count the number of objects in a bucket
			// so we can tell the server and expect all the new stuff
			last := int64(stats.KeyN - 1)
			p.Logf("querying %s since %d", server, last)

			// we send this and then expect the server to send
			// all available channels to us
			_, err = p.Client.Call("dev-sendcustommsg", server,
				strconv.FormatInt(MSG_REQUEST_CHANNELS, 16)+
					strconv.FormatInt(last, 16))
			if err != nil {
				p.Logf("error sending channels-request: %s", err.Error())
				return nil
			}

			return nil
		})
	}
}
