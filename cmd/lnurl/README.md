## The `lnurl` plugin.

Implements the wallet-side of the [lnurl spec](https://github.com/btcontract/lnurl-rfc/blob/master/spec.md), for interacting with lnurl-enabled services.

Provides two RPC commands:

 * `lnurlparams`, which prints all the parameters related to an `lnurl` (either taken from the querystring or fetched from the server).
 * `lnurl`, which performs the full lnurl flow for whatever the lnurl it's passed. There's no interaction, so all defaults are used unless you pass parameters.

![[](https://lnurl.bigsun.xyz/)](screencast.gif)

## How to install

This is distributed as a single binary for your delight (or you can compile it yourself with `go get`, or ask me for binaries for other systems if you need them).

[Download it](https://github.com/fiatjaf/lightningd-gjson-rpc/releases) and put it inside the `plugins/` directory of `lightning` folder (or `/usr/local/libexec/c-lightning/plugins/`, I guess, if installed with `sudo make install`) or start lightningd with `--plugin=/path/to/lnurl`.

You only need the binary you can get in [the releases page](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), nothing else.
