package main

import (
	"bytes"
	"fmt"
	"math/rand"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
)

var (
	verbose          = true
	blockUnavailable = map[string]interface{}{
		"blockhash": nil,
		"block":     nil,
	}
	esplora = []string{
		"https://mempool.ninja/electrs",
		"https://blockstream.info/api",
	}
)

func esploras() (ss []string) {
	n := len(esplora)
	index := rand.Intn(10)
	ss = make([]string, n)
	for i := 0; i < n; i++ {
		ss[i] = esplora[index%n]
	}
	return ss
}

func main() {
	p := plugin.Plugin{
		Name:    "trustedcoin",
		Version: "v0.1",
		Options: []plugin.Option{
			{
				"trustedcoin-verbose",
				"bool",
				true,
				"See how trustedcoin is being used before you decide to turn this off.",
			},
		},
		RPCMethods: []plugin.RPCMethod{
			{
				"getrawblockbyheight",
				"height",
				"Get the bitcoin block at a given height",
				"",
				func(p *plugin.Plugin, params plugin.Params) (resp interface{}, errCode int, err error) {
					height := params.Get("height").Int()
					if verbose {
						p.Logf("getting block %d", height)
					}

					block, hash, err := getBlock(height)
					if err != nil {
						return nil, 19, err
					}
					if block == "" {
						return blockUnavailable, 0, nil
					}

					if verbose {
						p.Logf("returning block %d, %s…, %d bytes", height, string(hash[:26]), len(block)/2)
					}

					return struct {
						BlockHash string `json:"blockhash"`
						Block     string `json:"block"`
					}{hash, string(block)}, 0, nil
				},
			}, {
				"getchaininfo",
				"",
				"Get the chain id, the header count, the block count and whether this is IBD.",
				"",
				func(p *plugin.Plugin, params plugin.Params) (resp interface{}, errCode int, err error) {
					tip, err := getTip()
					if err != nil {
						return nil, 20, fmt.Errorf("failed to get tip: %s", err.Error())
					}

					if verbose {
						p.Logf("tip: %d", tip)
					}

					return struct {
						Chain       string `json:"chain"`
						HeaderCount int64  `json:"headercount"`
						BlockCount  int64  `json:"blockcount"`
						IBD         bool   `json:"ibd"`
					}{"main", tip, tip, false}, 0, nil
				},
			}, {
				"getfeerate",
				"blocks [mode]",
				"Get the Bitcoin feerate in btc/kilo-vbyte.",
				"",
				func(p *plugin.Plugin, params plugin.Params) (resp interface{}, errCode int, err error) {
					blocks := params.Get("blocks").Int()

					rates, cacheHit, err := getFeeRates()
					if err != nil {
						p.Logf("failed to call btcpriceequivalent.com fee-estimates: %s", err.Error())
						return nil, 21, fmt.Errorf("failed to get fee estimates: %s", err.Error())
					}

					if verbose && !cacheHit {
						p.Logf("fees estimated: %v", rates)
					}

					feerate := rates[0].Amount
					for _, rate := range rates {
						if blocks < rate.N {
							break
						}
						feerate = rate.Amount
					}

					return struct {
						FeeRate int64 `json:"feerate"`
					}{feerate}, 0, nil
				},
			}, {
				"sendrawtransaction",
				"tx",
				"Send a raw transaction to the Bitcoin network.",
				"",
				func(p *plugin.Plugin, params plugin.Params) (resp interface{}, errCode int, err error) {
					hex := params.Get("tx").String()
					tx := bytes.NewBufferString(hex)

					srtresp, err := sendRawTransaction(tx)
					if err != nil {
						p.Logf("failed to publish transaction %s: %s", hex, err.Error())
						return nil, 21, err
					}

					p.Logf("sent raw transaction: %s", hex)

					return srtresp, 0, nil
				},
			}, {
				"getutxout",
				"txid vout",
				"Get informations about an output, identified by a {txid} an a {vout}",
				"",
				func(p *plugin.Plugin, params plugin.Params) (resp interface{}, errCode int, err error) {
					txid := params.Get("txid").String()
					vout := params.Get("vout").Int()

					tx, err := getTransaction(txid)
					if err != nil {
						p.Logf("failed to get tx %s: %s", txid, err.Error())
						return nil, 22, fmt.Errorf("failed to get tx %s: %s", txid, err.Error())
					}

					output := tx.Vout[vout]

					return struct {
						Amount int64  `json:"amount"`
						Script string `json:"script"`
					}{output.Value, output.ScriptPubKey}, 0, nil
				},
			},
		},
		OnInit: func(p *plugin.Plugin) {
			verbose = p.Args.Get("trustedcoin-verbose").Bool()
		},
	}

	p.Run()
}
