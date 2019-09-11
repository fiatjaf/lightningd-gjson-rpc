## The `waitpay` plugin.

Just call and wait until it pays. No more keep trying in the background in an asynchronous-unreliable manner such that you don't know if the payment succeeded or not or if it's still being tried. This is crucial for servers where withdrawals to untrusted third-parties are processed.

Provides two RPC commands:

 * `waitpay` command, similar to `pay`, but blocks and only resolves when the payment is either succeeded or failed. The parameters are mostly the same as parameters to `pay`. The return is always a `payment` object like the other returned from `listpayments`, either succeeded or failed, or it can be an error also, in bizarre cases.
* `waitpaystatus` command, similar to `paystatus`, but the return value is completely different: just an array of routes tried in the last call for the given `bolt11` invoice.

## How to install

This is distributed as a single binary for your delight (or you can compile it yourself with `go get`, or ask me for binaries for other systems if you need them).

[Download it](https://github.com/fiatjaf/lightningd-gjson-rpc/releases) and put it inside the `plugins/` directory of `lightning` folder (or `/usr/local/libexec/c-lightning/plugins/`, I guess, if installed with `sudo make install`) or start lightningd with `--plugin=/path/to/waitpay`.

You only need the binary you can get in [the releases page](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), nothing else.

## How to use

In your code that calls lightningd's RPC to make payments on behalf of untrusted user `userA` do something like this:

```python
try:
    freeze_user_funds(userA)
    payment = lightning.waitpay(bolt11invoice)
    if payment['status'] == 'complete':
        commit_user_payment(userA, bolt11invoice)
    elif payments['status'] == 'failed':
        unfreeze_user_funds(userA)
    else:
        throw Error("should never happen")
except Error:
    # should never happen
    # keep user funds frozen
    # resolve manually
    log('ERROR, BEWARE')
```

If a `should-never-happen` kind of error happens, please report the issue here.
