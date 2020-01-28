## The `routetracker` plugin.

This plugin uses the `rpc_command` hook to intercept your `sendpay` command. It doesn't modify it in any way, but it gathers the route used and sums the number of times each channel and node was used in a route, and amounts.

The data is stored in a database inside your `.lightning` directory.

Provides one RPC command:

 * `routestats` displays a JSON with output like the following:

```json
{
  "channels": [
    {
       "amount": 1337014,
       "channel": "606812x1936x1/1",
       "count": 10
    },
    ...
  ]
  "nodes": [
    {
       "amount": 585294363,
       "count": 12,
       "node": "03037dc08e9ac63b82581f79b662a4d0ceca8a8ca162b1af3551595b8f2d97b70a"
    },
    ...
  ]
}
```

## How to install

This is distributed as a single binary for your delight (or you can compile it yourself with `go get`, or ask me for binaries for other systems if you need them).

[Download it](https://github.com/fiatjaf/lightningd-gjson-rpc/releases) and put it inside the `plugins/` directory of `lightning` folder (or `/usr/local/libexec/c-lightning/plugins/`, I guess, if installed with `sudo make install`) or start lightningd with `--plugin=/path/to/routetracker`.

You only need the binary you can get in [the releases page](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), nothing else.

## How to use

Just install and forget.

Call `routestats` when you want to see the accumulated stats.

Also keep in mind that this tracks all routes that have been tried, not only routes that succeeded.
