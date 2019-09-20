## The `signatures` plugin.

Sign messages with your node's public key and verify signatures from others.

## How to install

This is distributed as a single binary for your delight (or you can compile it yourself with `go get`, or ask me for binaries for other systems if you need them).

[Download it](https://github.com/fiatjaf/lightningd-gjson-rpc/releases) and put it inside the `plugins/` directory of `lightning` folder (or `/usr/local/libexec/c-lightning/plugins/`, I guess, if installed with `sudo make install`) or start lightningd with `--plugin=/path/to/signatures`.

You only need the binary you can get in [the releases page](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), nothing else.

## How to use

```
lightning-cli sign 'vote trump'
{
   "hash": "9e22f6e74872d65dbd8cedf42bb702f432382cbdce04dd132043c7ceb6e61905",
   "pubkey": "02bed1812d3824f7cc4ccd38da5d66a29fcfec146fe95e26cd2e0d3f930d653a8d",
   "signature": "3045022100d4e623a6dcb0d9728e612fec8d35bfae18b7936574d8b7cc3ff1e4b9680c38d102202e671c1c6c1ff6fcb63a51d77fd32fff52f235dc1fff108e2219179b3923c9f7",
   "was_hashed": true
}
lightning-cli sign 9e22f6e74872d65dbd8cedf42bb702f432382cbdce04dd132043c7ceb6e61905

{
   "hash": "9e22f6e74872d65dbd8cedf42bb702f432382cbdce04dd132043c7ceb6e61905",
   "pubkey": "02bed1812d3824f7cc4ccd38da5d66a29fcfec146fe95e26cd2e0d3f930d653a8d",
   "signature": "3045022100d4e623a6dcb0d9728e612fec8d35bfae18b7936574d8b7cc3ff1e4b9680c38d102202e671c1c6c1ff6fcb63a51d77fd32fff52f235dc1fff108e2219179b3923c9f7",
   "was_hashed": false
}
lightning-cli verify 9e22f6e74872d65dbd8cedf42bb702f432382cbdce04dd132043c7ceb6e61905 3045022100d4e623a6dcb0d9728e612fec8d35bfae18b7936574d8b7cc3ff1e4b9680c38d102202e671c1c6c1ff6fcb63a51d77fd32fff52f235dc1fff108e2219179b3923c9f7 02bed1812d3824f7cc4ccd38da5d66a29fcfec146fe95e26cd2e0d3f930d653a8d
{
   "hash": "9e22f6e74872d65dbd8cedf42bb702f432382cbdce04dd132043c7ceb6e61905",
   "valid": true,
   "was_hashed": false
}
lightning-cli verify 'vote trump' 3045022100d4e623a6dcb0d9728e612fec8d35bfae18b7936574d8b7cc3ff1e4b9680c38d102202e671c1c6c1ff6fcb63a51d77fd32fff52f235dc1fff108e2219179b3923c9f7 02bed1812d3824f7cc4ccd38da5d66a29fcfec146fe95e26cd2e0d3f930d653a8d
{
   "hash": "9e22f6e74872d65dbd8cedf42bb702f432382cbdce04dd132043c7ceb6e61905",
   "valid": true,
   "was_hashed": true
}
```
