## The `jqmethods` plugin.

This plugin provides an easy way to call _read-only_ methods that take output from lightningd and postprocess them with [jq](https://stedolan.github.io/jq/). You know, raw `lightning-cli` output can be a little overwhelming sometimes and we end up not finding the stuff we want formatted the way we need at all times. It's also a bad idea to write complex `jq` expressions every time you need to know what is the percentage of empty channels you have or what's the average channel size of the entire network you can see, I'm just making up these examples.

What we call a _jq method_ is a combination of one or more lightningd RPC calls with a `jq` expression that takes that data and postprocesses it.

The idea is that you can either rely on a centrally-managed manually-curated collection of generally-useful _jq methods_ or just use your own set of dirty hacks, as long as they are exposed in a GitHub repository in the expected format. See [WRITE.md](WRITE.md) for more information on how to write methods yourself.

## How to install

This is a single binary for your delight (currently linux/amd64, or you can compile it yourself with `go get`, or ask me for binaries for other systems if you need them).

[Download it](https://github.com/fiatjaf/lightningd-gjson-rpc/releases) and put it inside the `plugins/` directory of `lightning` folder (or `/usr/local/libexec/c-lightning/plugins/`, I guess, if installed with `sudo make install`) or start lightningd with `--plugin=/path/to/jqmethods`.

You only need the binary you can get in [the releases page](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), nothing else.

## How to use

When starting `lightningd` you can pass `--jq-source=user/repo` (pointing to a GitHub repository) and any `.yaml` file there will be fetched and made available locally (you can also write that in your `~/.lightning/config` file). By default methods will be fetched from [fiatjaf/jqmethods](https://github.com/fiatjaf/jqmethods). You're welcome to publish your methods there if they are reasonable, please open a pull request!

To call a jq method do `lightning-cli jq <methodname> [args...]`. For example:

```
lightning-cli jq channel_balance_status
```

```json
[
   {
      "short_channel_id" : "587204x2215x0",
      "us" : 68095.144,
      "them" : 9831904.856,
      "status" : [
         "CHANNELD_NORMAL:Reconnected, and reestablished.",
         "CHANNELD_NORMAL:Funding transaction locked. Waiting for their announcement signatures."
      ],
      "balance" : "low here"
   },
   {
      "short_channel_id" : "564354x2724x0",
      "us" : 1278829.782,
      "them" : 564467.218,
      "status" : [
         "CHANNELD_NORMAL:Reconnected, and reestablished.",
         "CHANNELD_NORMAL:Funding transaction locked. Channel announced."
      ],
      "balance" : "low there"
   }
]
```

Calling just `jq` will list all available methods.  Calling `jq-refresh` will refresh the set of available methods without having to reload `lightningd`.

Calling `jq-refresh` will reload the set of available methods from the same source without you having to restart lightningd.

## Methods currently available automatically

[![](https://raw.githubusercontent.com/fiatjaf/jqmethods/master/methods.png)](https://github.com/fiatjaf/jqmethods)
