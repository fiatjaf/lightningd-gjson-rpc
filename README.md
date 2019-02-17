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
    ln, err = lightning.Connect("/home/whatever/.lightning/lightning-rpc")
    if err != nil {
        log.Fatal("couldn't connect to lightning-rpc")
    }
    ln.LastInvoiceIndex = 0
    ln.PaymentHandler = handlePaymentReceived
    ln.ListenForInvoices()

    nodeinfo, err := ln.Call("getinfo")
    if err != nil {
        log.Fatal("getinfo timeout")
    }

    log.Print(nodeinfo.Get("alias").String())
}

// this is the result of `waitanyinvoice`
func handlePaymentReceived(inv gjson.Result) {
    // save this somewhere so we can start from here next time
    index := inv.Get("pay_index").Int()

    hash := inv.Get("payment_hash")
    log.Print("one of our invoices was paid: " + hash)
}
```
