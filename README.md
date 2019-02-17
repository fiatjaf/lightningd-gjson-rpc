Simple interface for [lightningd](https://github.com/ElementsProject/lightning/). All methods return [gjson.Result](https://github.com/tidwall/gjson).

Comes with a practical _invoice listener_ method.

[godoc.org](https://godoc.org/github.com/fiatjaf/lightningd-gjson-rpc)

Usage:

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

    hash := inv.Get("payment_hash")
    log.Print("one of our invoices was paid: " + hash)
}
```
