## The `lnurl` plugin.

Implements the wallet-side of the [lnurl spec](https://github.com/btcontract/lnurl-rfc/blob/master/spec.md), for interacting with lnurl-enabled services.

Provides two RPC commands:

 * `lnurlparams`, which prints all the parameters related to an `lnurl` (either taken from the querystring or fetched from the server).
 * `lnurl`, which performs the full lnurl flow for whatever the lnurl it's passed. There's no interaction, so all defaults are used unless you pass parameters.

![[](https://lnurl.bigsun.xyz/)](screencast.gif)

## How to install

This is distributed as a single binary for your delight (or you can compile it yourself with `go get`, or ask me for binaries for other systems if you need them).

[Download it](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), call `chmod +x <binary>` and put it inside the `plugins/` directory of `lightning` folder (or `/usr/local/libexec/c-lightning/plugins/` if installed with `sudo make install`) or start lightningd with `--plugin=/path/to/lnurl`.

You only need the binary you can get in [the releases page](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), nothing else.

## How to use

### Getting a channel from lnbig.com:
  1. Go to https://lnbig.com/
  2. On "Inbound channel on you" select "Bitcoin Lightning Wallet (aka BLW for Android)", click Next
  3. Copy the link from the QR code that will appear (right-click and use the context menu), it will be something like `lightning:lnurl1dp68gurn8ghj7mrwvf5kwtnrdakj7ctsdyhhvvf0d3h82unv8a6h26ty843kvcmyv93kyc3dvscrvvpdx3nrvvpd8quxgvedxymkxctpxe3kxvtxx4nq2ujkf8`
  4. Call `lightning-cli lnurl lightning:lnurl1dp68gurn8ghj7mrwvf5kwtnrdakj7ctsdyhhvvf0d3h82unv8a6h26ty843kvcmyv93kyc3dvscrvvpdx3nrvvpd8quxgvedxymkxctpxe3kxvtxx4nq2ujkf8`
  5. If the call was successful you don't have to do anything, lnbig.com will open a channel with your node.

### Withdrawing from a service:
  1. Create a gift at https://lightning.gifts/
  2. On the gift redeem page, where it says "Scan or click QR code with an LNURL-compatible wallet", copy the link from the QR code (using the mouse context menu), it will be something like `lightning:lnurl1dp68gurn8ghj7ctsdyhxc6t8dp6xu6twvuhxw6txw3ej7mrww4exctmpv5crxdfcxg6nvwryxe3kgdp4xy6xyvekv5ervwfevv6x2vp3x3jnydrpv3nrvwfcx3jkzvp5v93s5uyl7r`
  3. Call `lightning-cli lnurl lightning:lnurl1dp68gurn8ghj7ctsdyhxc6t8dp6xu6twvuhxw6txw3ej7mrww4exctmpv5crxdfcxg6nvwryxe3kgdp4xy6xyvekv5ervwfevv6x2vp3x3jnydrpv3nrvwfcx3jkzvp5v93s5uyl7r`
  4. In the answer, you'll get the label of the invoice that was created, so you can call `lightning-cli waitinvoice ...` on it.

### Grabbing the lnurl params from an lnurl so you can decide manually what to do with them
  1. Call `lightning-cli lnurlparams lnurl1...`
  2. You'll get an answer like `{"status":"","tag":"withdrawRequest","k1":"ae03582568d6cd4514b36e2699c4e014e24adf6984ea04ac","callback":"https://api.lightning.gifts/lnurl/ae03582568d6cd4514b36e2699c4e014e24adf6984ea04ac","maxWithdrawable":123000,"minWithdrawable":123000,"defaultDescription":"lightning.gifts redeem ae03582568d6cd4514b36e2699c4e014e24adf6984ea04ac"}` if it's a lnurl-withdraw, different if it's another kind of lnurl.

### Encoding an URL from your own service as bech32 with "lnurl" prefix (if you're implementing server-side lnurl support in your app)
  1. Call `lightning-cli lnurlencode https://myservice.com/lnurl/something`
  2. You'll get an answer like `{"lnurl": "lnurl1..."}`
