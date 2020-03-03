package main

import (
	"time"

	"github.com/coreos/bbolt"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
)

func getUpdates(p *plugin.Plugin, server string) {
	// every 15 minutes we'll query this server for new channels
	// and save them
	for {
		db.Update(func(tx *bbolt.Tx) error {
			bucket := tx.Bucket([]byte("channels"))

			last, _ := bucket.Cursor().Last()
			p.Logf("querying since %d", last)

			// for _, channels := range res {
			// 	next, _ := bucket.NextSequence()
			// 	p.Logf("saving channel %v at sequence %d", channel, next)
			// }

			return nil
		})

		time.Sleep(15 * time.Minute)
	}
}
