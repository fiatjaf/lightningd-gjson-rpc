package main

import (
	"encoding/json"

	"github.com/coreos/bbolt"
	lightning "github.com/fiatjaf/lightningd-gjson-rpc"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func listchannels(p *plugin.Plugin, rpc_command gjson.Result) interface{} {
	if len(rpc_command.Get("params").String()) > 2 {
		// if args are passed then just return TODO
		return continuehook
	}

	// increment the result from the normal listchannels with channels from our
	// chanstore peers and our own
	res, err := p.Client.Call("listchannels")
	if err != nil {
		return continuehook
	}
	channels := res.Get("channels").String()

	// buckets to look in
	buckets := make([][]byte, 0, len(serverlist)+1)
	for server, _ := range serverlist {
		buckets = append(buckets, []byte(server))
	}
	buckets = append(buckets, []byte("server"))

	db.View(func(tx *bbolt.Tx) error {
		for _, bucketName := range buckets {
			bucket := tx.Bucket(bucketName)
			bucket.ForEach(func(_, v []byte) error {
				var channel lightning.Channel
				err := json.Unmarshal(v, &channel)
				if err != nil {
					p.Logf("error unmarshaling extra channel (%s): %s",
						string(v), err.Error())
					return nil
				}
				channels, _ = sjson.Set(channels, "-1", channel)
				return nil
			})
		}
		return nil
	})

	return map[string]interface{}{
		"return": map[string]interface{}{
			"result": map[string]interface{}{
				"channels": channels,
			},
		},
	}
}
