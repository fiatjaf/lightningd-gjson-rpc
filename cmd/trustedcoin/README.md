## The `trustedcoin` plugin.

A plugin that uses block explorers (blockstream.info, mempool.space, blockchair.com and blockchain.info) as backends instead of your own Bitcoin node.

This isn't what you should be doing, but sometimes you may need it.

(Remember this will download all blocks c-lightning needs from blockchain.info or blockchair.com in raw, hex format.)

## How to install

This is distributed as a single binary for your delight (or you can compile it yourself with `go get`, or ask me for binaries for other systems if you need them).

[Download it](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), call `chmod +x <binary>` and put it inside the `plugins/` directory of `lightning` folder (or `/usr/local/libexec/c-lightning/plugins/` if installed with `sudo make install`) or start lightningd with `--plugin=/path/to/trustedcoin`.

You only need the binary you can get in [the releases page](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), nothing else.

Also call `chmod -x bcli` so the `bcli` plugin that comes installed by default doesn't conflict with `trustedcoin`.

## How to bootstrap a Lightning node from scratch, without Bitcoin Core, on Ubuntu amd64

```
add-apt-repository ppa:lightningnetwork/ppa
apt update
apt install lightningd
cd /usr/libexec/c-lightning/plugins
chmod -x bcli
wget https://github.com/fiatjaf/lightningd-gjson-rpc/releases/download/trustedcoin-v0.2/trustedcoin_linux_amd64
chmod +x trustedcoin_linux_amd64
cd
lightningd
```

## How to use

You don't have to do anything, this will just work.
