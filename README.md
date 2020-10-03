Useful and fast interface for [lightningd](https://github.com/ElementsProject/lightning/). All methods return [`gjson.Result`](https://godoc.org/github.com/tidwall/gjson#Result), which is a good thing and very fast. [Try the GJSON playground to learn](https://gjson.dev/).

Since RPC calls are just relayed and wrapped, you can use **lightningd-gjson-rpc** to call [custom RPC methods](https://lightning.readthedocs.io/PLUGINS.html) if your node has a plugin enabled on it.

[![godoc.org](https://img.shields.io/badge/reference-godoc-blue.svg)](https://godoc.org/github.com/fiatjaf/lightningd-gjson-rpc)

This is a simple and resistant client. It is made to survive against faulty **lightning** node interruptions. It can also talk to [spark](https://github.com/shesek/spark-wallet)/[sparko](https://github.com/fiatjaf/lightningd-gjson-rpc/tree/master/cmd/sparko#client-libraries) HTTP-RPC using the same API, so you can run your app and your node on different machines.

## Usage

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
        LastInvoiceIndex: lastinvoiceindex, // only needed if you're going to listen for invoices
        PaymentHandler:   handleInvoicePaid, // only needed if you're going to listen for invoices
        CallTimeout: 10 * time.Second, // optional, defaults to 5 seconds
    }
    ln.ListenForInvoices() // optional

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

### Passing parameters

There are three modes of passing parameters, you can call either:

```go
// 1. `Call` with a list of parameters, in the order defined by each command;
ln.Call("invoice", 1000000, "my-label", "my description", 3600)

// 2. `Call` with a single `map[string]interface{}` with all parameters properly named; or
ln.Call("invoice", map[string]interface{
    "msatoshi": "1000000,
    "label": "my-label",
    "description": "my description",
    "preimage": "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
})

// 3. `CallNamed` with a list of keys and values passed in the proper order.
ln.CallNamed("invoice",
    "msatoshi", "1000000,
    "label", "my-label",
    "description", "my description",
    "preimage", "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
    "expiry", 3600,
)
```

## Special methods

Besides providing full access to the c-lightning RPC interface with `.Call` methods, we also have [ListenForInvoices](https://godoc.org/github.com/fiatjaf/lightningd-gjson-rpc#Client.ListenForInvoices), [PayAndWaitUntilResolution](https://godoc.org/github.com/fiatjaf/lightningd-gjson-rpc#Client.PayAndWaitUntilResolution) and [GetPrivateKey](https://godoc.org/github.com/fiatjaf/lightningd-gjson-rpc#Client.GetPrivateKey) to make your life better.

It's good to say also that since we don't have hardcoded methods here you can call [custom RPC methods](https://lightning.readthedocs.io/PLUGINS.html#json-rpc-passthrough) with this library.

## Plugins

If you want to write a plugin, we provide [helpers](plugin) to make that easy. Take a look at https://github.com/fiatjaf/sparko or https://github.com/fiatjaf/lightningd-webhook for examples.
