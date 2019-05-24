## The `webhook` plugin.

This plugin sends a webhook to any URL when a new payment is received by your node.

## How to install

This is a single binary for your delight (currently linux/amd64, or you can compile it yourself with `go get`, or ask me for binaries for other systems if you need them).

[Download it](https://github.com/fiatjaf/lightningd-gjson-rpc/releases) and put it inside the `plugins/` directory of `lightning` folder (or `/usr/local/libexec/c-lightning/plugins/`, I guess, if installed with `sudo make install`) or start lightningd with `--plugin=/path/to/webhook`.

You only need the binary you can get in [the releases page](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), nothing else.

## How to use

Initialize `lightningd` passing the option `--webhook=https://your.url/here` and that's it. You can also write that in your `~/.lightning/config` file. A nice place to get webhook endpoints for a quick test is https://beeceptor.com/.
