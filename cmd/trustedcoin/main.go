package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
)

var (
	verbose          = true
	blockUnavailable = map[string]interface{}{
		"blockhash": nil,
		"block":     nil,
	}
)

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
					blockhash, err := getHash(height)
					if err != nil {
						return nil, 18, fmt.Errorf("failed to get %d hash: %s", height, err.Error())
					}

					if blockhash == "" {
						// block not mined yet
						return blockUnavailable, 0, nil
					}

					if verbose {
						p.Logf("getting block %d: %s", height, blockhash)
					}
					w, err := http.Get(fmt.Sprintf("https://blockchain.info/block/%s?format=hex", blockhash))
					if err != nil {
						return nil, 19, fmt.Errorf("failed to get raw block %d from blockchain.com: %s", height, err.Error())
					}
					defer w.Body.Close()

					block, _ := ioutil.ReadAll(w.Body)
					if len(block) < 100 {
						// block not available on blockchain.info yet
						if verbose {
							p.Logf("block %d not available yet", height)
						}
						return blockUnavailable, 0, nil
					}

					if verbose {
						p.Logf("returning block %d: %s...", height, string(block[:50]))
					}

					return struct {
						BlockHash string `json:"blockhash"`
						Block     string `json:"block"`
					}{blockhash, string(block)}, 0, nil
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

					w, err := http.Post("https://blockstream.info/api/tx", "text/plain", tx)
					if err != nil {
						p.Logf("failed to call publish transaction on blockstream.info: %s", hex, err.Error())
						return nil, 21, err
					}
					defer w.Body.Close()

					p.Logf("sent raw transaction: %s", hex)

					if w.StatusCode >= 300 {
						msg, _ := ioutil.ReadAll(w.Body)
						return struct {
							Success bool   `json:"success"`
							ErrMsg  string `json:"errmsg"`
						}{false, string(msg)}, 0, nil
					}

					return struct {
						Success bool   `json:"success"`
						ErrMsg  string `json:"errmsg"`
					}{true, ""}, 0, nil
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

func getTip() (int64, error) {
	w, err := http.Get("https://blockstream.info/api/blocks/tip/height")
	if err != nil {
		return 0, err
	}
	defer w.Body.Close()

	data, err := ioutil.ReadAll(w.Body)
	if err != nil {
		return 0, err
	}

	tip, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return 0, err
	}

	return tip, nil
}

func getHash(height int64) (string, error) {
	w, err := http.Get(fmt.Sprintf("https://blockstream.info/api/block-height/%d", height))
	if err != nil {
		return "", err
	}
	defer w.Body.Close()

	if w.StatusCode >= 404 {
		return "", nil
	}

	data, err := ioutil.ReadAll(w.Body)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func getTransaction(txid string) (tx struct {
	TXID string `json:"txid"`
	Vout []struct {
		ScriptPubKey string `json:"scriptPubKey"`
		Value        int64  `json:"value"`
	} `json:"vout"`
}, err error) {
	w, err := http.Get("https://blockstream.info/api/tx/" + txid)
	if err != nil {
		return
	}
	defer w.Body.Close()

	err = json.NewDecoder(w.Body).Decode(&tx)
	return
}

type FeeRate struct {
	N      int64 `json:"n"`
	Amount int64 `json:"amount"`
}

var feeCache []FeeRate
var feeCacheTime = time.Now().Add(-time.Hour * 1)

func getFeeRates() (rates []FeeRate, cacheHit bool, err error) {
	if feeCacheTime.After(time.Now().Add(-time.Minute * 10)) {
		return feeCache, true, nil
	}

	w, err := http.Get("https://btcpriceequivalent.com/fee-estimates")
	if err != nil {
		return nil, false, err
	}

	defer w.Body.Close()
	data, _ := ioutil.ReadAll(w.Body)

	sep := bytes.Index(data, []byte{'='})
	err = json.Unmarshal(data[sep+1:], &rates)
	if err != nil {
		return nil, false, err
	}

	feeCache = rates
	feeCacheTime = time.Now()

	return rates, false, nil
}
