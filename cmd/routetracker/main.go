package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/tidwall/buntdb"
	"github.com/tidwall/gjson"
)

func main() {
	p := plugin.Plugin{
		Name:    "routetracker",
		Version: "v0.1",
		RPCMethods: []plugin.RPCMethod{
			{
				"routestats",
				"",
				"View channels and nodes most used in sent payments.",
				"",
				func(p *plugin.Plugin, params plugin.Params) (interface{}, int, error) {
					db, err := buntdb.Open("routetracker.db")
					if err != nil {
						return map[string]interface{}{"return": map[string]interface{}{
							"error": "failed to open database at routetracker.db",
						}}, 4, nil
					}
					defer db.Close()

					result := make(map[string]interface{})
					db.View(func(tx *buntdb.Tx) error {
						channels := make(map[string]map[string]interface{})
						nodes := make(map[string]map[string]interface{})
						tx.Ascend("", func(key, value string) bool {
							val := gjson.Parse(value).Int()

							spl := strings.Split(key, ":")
							switch strings.Index(spl[1], "/") != -1 {
							case true:
								channel, ok := channels[spl[1]]
								if !ok {
									channel = map[string]interface{}{
										"channel": spl[1],
										"amount":  0,
										"count":   0,
									}
								}
								switch spl[0] {
								case "c":
									channel["count"] = val
								case "a":
									channel["amount"] = val
								}
								channels[spl[1]] = channel
							case false:
								node, ok := nodes[spl[1]]
								if !ok {
									node = map[string]interface{}{
										"node":   spl[1],
										"amount": 0,
										"count":  0,
									}
								}
								switch spl[0] {
								case "c":
									node["count"] = val
								case "a":
									node["amount"] = val
								}
								nodes[spl[1]] = node
							}

							return true
						})
						schannels := make([]map[string]interface{}, len(channels))
						snodes := make([]map[string]interface{}, len(nodes))

						i := 0
						for _, v := range channels {
							schannels[i] = v
							i++
						}

						i = 0
						for _, v := range nodes {
							snodes[i] = v
							i++
						}

						sort.Slice(schannels, func(i, j int) bool {
							return schannels[i]["count"].(int64) < schannels[j]["count"].(int64)
						})
						sort.Slice(snodes, func(i, j int) bool {
							return snodes[i]["count"].(int64) < snodes[j]["count"].(int64)
						})

						result["channels"] = schannels
						result["nodes"] = snodes
						return nil
					})

					return result, 0, nil
				},
			},
		},
		Hooks: []plugin.Hook{
			{
				"rpc_command",
				func(p *plugin.Plugin, params plugin.Params) (resp interface{}) {
					data := params.Get("rpc_command.rpc_command")
					// store sendpay statistics.
					db, err := buntdb.Open("routetracker.db")
					if err != nil {
						p.Logf("failed to open database at routetracker.db")
						goto end
					}
					defer db.Close()

					if data.Get("method").String() == "sendpay" {
						db.Update(func(tx *buntdb.Tx) error {
							data.Get("params.route").ForEach(func(_, v gjson.Result) bool {
								channel := v.Get("channel").String() + "/" +
									v.Get("direction").String()
								node := v.Get("id").String()
								msatoshi := v.Get("msatoshi").Int()

								ccount, _ := tx.Get("c:" + channel)
								camt, _ := tx.Get("a:" + channel)
								ncount, _ := tx.Get("c:" + node)
								namt, _ := tx.Get("a:" + node)

								ccount = fmt.Sprintf("%d", gjson.Parse(ccount).Int()+1)
								ncount = fmt.Sprintf("%d", gjson.Parse(ncount).Int()+1)
								camt = fmt.Sprintf("%d", gjson.Parse(camt).Int()+msatoshi)
								namt = fmt.Sprintf("%d", gjson.Parse(namt).Int()+msatoshi)

								tx.Set("c:"+channel, ccount, nil)
								tx.Set("a:"+channel, camt, nil)
								tx.Set("c:"+node, ncount, nil)
								tx.Set("a:"+node, namt, nil)

								return true
							})
							return nil
						})
					}

				end:
					return map[string]interface{}{
						"continue": true,
					}
				},
			},
		},
		Dynamic: true,
	}

	p.Run()
}
