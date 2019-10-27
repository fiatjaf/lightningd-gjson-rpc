## The `shadows` plugin.

This plugin allows you to create an invoice from a private node that is linked to yours through a private channel. The catch is that that node doesn't exist nor the channel, and still you can receive that invoice yourself while the payer thinks he is paying a private node.

Provides one RPC command:

 * `shadow-invoice` command, similar to `invoice`. However be sure to select a unique `id` every time as that will be used to generate the invoice preimage and shadow channel id which are then used later by the `htlc_accepted` hook to shortcut that invoice payment.

## How to install

This is distributed as a single binary for your delight (or you can compile it yourself with `go get`, or ask me for binaries for other systems if you need them).

[Download it](https://github.com/fiatjaf/lightningd-gjson-rpc/releases) and put it inside the `plugins/` directory of `lightning` folder (or `/usr/local/libexec/c-lightning/plugins/`, I guess, if installed with `sudo make install`) or start lightningd with `--plugin=/path/to/shadows`.

You only need the binary you can get in [the releases page](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), nothing else.

## How to use

Create an invoice like the following:

```
~> lightning-cli shadow-invoice 10000 uniqueid 'this invoice is almost real'
```

Then pay it from another node.

## Gotchas

For now, you don't get any notification anywhere about the received invoice (only a log entry emitted by this plugin). This will improve in the future.
