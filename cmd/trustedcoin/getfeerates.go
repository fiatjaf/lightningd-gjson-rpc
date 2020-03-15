package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"strconv"
	"time"
)

type FeeRate struct {
	N      int64 `json:"n"`
	Amount int64 `json:"amount"`
}

type FeeRates []FeeRate

func (fr FeeRates) Len() int           { return len(fr) }
func (fr FeeRates) Less(i, j int) bool { return fr[i].N < fr[j].N }
func (fr FeeRates) Swap(i, j int)      { fr[i], fr[j] = fr[j], fr[i] }

var feeCache FeeRates
var feeCacheTime = time.Now().Add(-time.Hour * 1)

func getFeeRates() (rates FeeRates, cacheHit bool, err error) {
	if feeCacheTime.After(time.Now().Add(-time.Minute * 1)) {
		return feeCache, true, nil
	}

	for _, try := range []func() (FeeRates, error){feeFromEsplora, feeFromBTCPriceEquivalent} {
		rates, err = try()
		if err != nil {
			continue
		}
		break
	}

	sort.Sort(rates)
	feeCache = rates
	feeCacheTime = time.Now()

	return rates, false, nil
}

func feeFromBTCPriceEquivalent() (FeeRates, error) {
	w, err := http.Get("https://btcpriceequivalent.com/fee-estimates")
	if err != nil {
		return nil, err
	}

	defer w.Body.Close()
	data, _ := ioutil.ReadAll(w.Body)

	var rates FeeRates
	sep := bytes.Index(data, []byte{'='})
	err = json.Unmarshal(data[sep+1:], &rates)
	if err != nil {
		return nil, err
	}
	if len(rates) < 8 {
		return nil, errors.New("invalid response from btcpriceequivalent.com")
	}

	return rates, nil
}

func feeFromEsplora() (rates FeeRates, err error) {
	for _, endpoint := range esploras() {
		u := endpoint + "/fee-estimates"
		w, err := http.Get(u)
		if err != nil {
			return nil, err
		}
		if w.StatusCode >= 300 {
			return nil, fmt.Errorf("%s returned an error code: %d", u, w.StatusCode)
		}

		defer w.Body.Close()
		data, _ := ioutil.ReadAll(w.Body)

		estimates := make(map[string]float64)
		err = json.Unmarshal(data, &estimates)
		if err != nil {
			return nil, err
		}

		rates := make(FeeRates, len(estimates))
		i := 0
		for sblocks, sats := range estimates {
			var blocks int64
			blocks, err = strconv.ParseInt(sblocks, 10, 64)
			if err != nil {
				break
			}

			rates[i] = FeeRate{
				N:      blocks,
				Amount: int64(sats),
			}
			i++
		}

		if len(rates) < 9 {
			err = errors.New("invalid response from " + u)
			break
		}
		return rates, nil
	}

	return
}
