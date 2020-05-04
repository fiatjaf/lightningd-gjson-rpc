## The `trustedcoin` plugin.

A plugin that uses block explorers (blockstream.info, mempool.space, blockchain.com and blockchain.info) as backends instead of your own Bitcoin node.

This isn't what you should be doing, but sometimes you may need it.

(Remember this will download all blocks c-lightning needs from blockchain.info in raw, hex format.)

## How to install

This is distributed as a single binary for your delight (or you can compile it yourself with `go get`, or ask me for binaries for other systems if you need them).

[Download it](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), call `chmod +x <binary>` and put it inside the `plugins/` directory of `lightning` folder (or `/usr/local/libexec/c-lightning/plugins/` if installed with `sudo make install`) or start lightningd with `--plugin=/path/to/trustedcoin`.

You only need the binary you can get in [the releases page](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), nothing else.

Also call `chmod -x bcli` so the `bcli` plugin that comes installed by default doesn't conflict with `trustedcoin`.

## How to use

You don't have to do anything, this will just work.
