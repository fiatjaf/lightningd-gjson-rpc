package main

import (
	"strconv"
	"strings"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/tidwall/gjson"
)

func getroute(p *plugin.Plugin, rpc_command gjson.Result) interface{} {
	iparams := rpc_command.Get("params").Value()
	var parsed plugin.Params
	switch params := iparams.(type) {
	case []interface{}:
		parsed, err = plugin.GetParams(
			params,
			"id msatoshi riskfactor [cltv] [fromid] [fuzzpercent] [exclude] [maxhops]",
		)
		if err != nil {
			p.Log("failed to parse getroute parameters: %s", err)
			return continuehook
		}
	case map[string]interface{}:
		parsed = plugin.Params(params)
	}

	exc := parsed.Get("exclude").Array()
	exclude := make([]string, len(exc))
	for i, chandir := range exc {
		exclude[i] = chandir.String()
	}

	fuzz := parsed.Get("fuzzpercent").String()
	fuzzpercent, err := strconv.ParseFloat(fuzz, 64)
	if err != nil {
		fuzzpercent = 5.0
	}

	fromid := parsed.Get("fromid").String()
	if fromid == "" {
		res, err := p.Client.Call("getinfo")
		if err != nil {
			p.Logf("can't get our own nodeid: %w", err)
			return continuehook
		}
		fromid = res.Get("id").String()
	}

	cltv := parsed.Get("cltv").Int()
	if cltv == 0 {
		cltv = 9
	}

	maxhops := int(parsed.Get("maxhops").Int())
	if maxhops == 0 {
		maxhops = 20
	}

	msatoshi := parsed.Get("msatoshi").Int()
	if msatoshi == 0 {
		for _, suffix := range []string{"msat", "sat", "btc"} {
			msatoshiStr := parsed.Get("msatoshi").String()
			spl := strings.Split(msatoshiStr, suffix)
			if len(spl) == 2 {
				amt, err := strconv.ParseFloat(spl[0], 10)
				if err != nil {
					return map[string]interface{}{
						"return": map[string]interface{}{
							"error": "failed to parse " + msatoshiStr,
						},
					}
				}
				msatoshi = int64(amt * multipliers[suffix])
				break
			}
		}
	}

	if msatoshi == 0 {
		return map[string]interface{}{
			"return": map[string]interface{}{
				"error": "msatoshi can't be 0",
			},
		}
	}

	target := parsed.Get("id").String()
	riskfactor := parsed.Get("riskfactor").Int()

	p.Logf("querying route from %s to %s for %d msatoshi with riskfactor %d, fuzzpercent %f, excluding %v", fromid, target, msatoshi, riskfactor, fuzzpercent, exclude)

	route, err := p.Client.GetRoute(
		target,
		msatoshi,
		riskfactor,
		cltv,
		fromid,
		fuzzpercent,
		exclude,
		maxhops,
		0.5,
	)

	if err != nil {
		p.Logf("failed to getroute: %s, falling back to default.", err)
		return continuehook
	}

	return map[string]interface{}{
		"return": map[string]interface{}{
			"result": map[string]interface{}{
				"route": route,
			},
		},
	}
}
