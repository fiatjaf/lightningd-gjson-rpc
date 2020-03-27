package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"sort"
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

	for _, try := range []func() (FeeRates, error){feeFromBTCPriceEquivalent} {
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
