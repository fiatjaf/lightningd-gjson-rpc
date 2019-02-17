Useful and fast interface for [lightningd](https://github.com/ElementsProject/lightning/). All methods return [gjson.Result](https://godoc.org/github.com/tidwall/gjson#Result), which is a good thing (unless your application relies on a lot of information from many different lightningd responses) and you can [learn it in 10 seconds](https://github.com/tidwall/gjson#get-a-value).

[![godoc.org](https://img.shields.io/badge/reference-godoc-blue.svg)](https://godoc.org/github.com/fiatjaf/lightningd-gjson-rpc)

This is a simple and resistant client. It comes with a practical **invoice listener** method and nice (could be nicer?) defaults for retrying and connecting to faulty lightningd nodes.

### Usage

```go
package main

import (
  "github.com/fiatjaf/lightningd-gjson-rpc"
  "github.com/tidwall/gjson"
)

var ln *lightning.Client

func main () {
    lastinvoiceindex := getFromSomewhereOrStartAtZero()

    ln = &lightning.Client{
        Path:             "/home/whatever/.lightning/lightning-rpc",
        LastInvoiceIndex: lastinvoiceindex,
        PaymentHandler:   handleInvoicePaid,
    }
    ln.ListenForInvoices()

    nodeinfo, err := ln.Call("getinfo")
    if err != nil {
        log.Fatal("getinfo error: " + err.Error())
    }

    log.Print(nodeinfo.Get("alias").String())
}

// this is called with the result of `waitanyinvoice`
func handlePaymentReceived(inv gjson.Result) {
    index := inv.Get("pay_index").Int()
    saveSomewhere(index)

    hash := inv.Get("payment_hash").String()
    log.Print("one of our invoices was paid: " + hash)
}
```
