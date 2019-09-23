## The `sparko` plugin.

The famous [Spark wallet](https://github.com/shesek/spark-wallet) repackaged as a single-binary plugin.

Plus some differences (advantages?):

* Single binary: No dependencies to manage, just grab the manage and throw it in your `lightningd` plugins folder.
* Runs as a plugin: this means you don't have to manage the server, it will be managed by `lightningd` and will always be running as long as your node is running.
* Centralized options management: since it runs as a plugin all options are read from your `lightningd` config file.
* Written in Go: much faster than Node.js, doesn't require that painful runtime to be running alongside your node, it's very lean.
* Unrestricted: any method can be called through the HTTP/JSON-RPC interface, including any methods provided by plugins you might have active in your node.
* No default login: you don't have to expose "super user" credentials over your node. You can have only access-keys to specific methods. But you can define a login an password too, of course.

## How to install

This is distributed as a single binary for your delight (or you can compile it yourself with `go get`, or ask me for binaries for other systems if you need them).

[Download it](https://github.com/fiatjaf/lightningd-gjson-rpc/releases) and put it inside the `plugins/` directory of `lightning` folder (or `/usr/local/libexec/c-lightning/plugins/`, I guess, if installed with `sudo make install`) or start lightningd with `--plugin=/path/to/sparko`.

You only need the binary you can get in [the releases page](https://github.com/fiatjaf/lightningd-gjson-rpc/releases), nothing else.

## How to use

Just configure the options you want in you `~/.lightning/config` file, like the following:

```
sparko-tls-path=sparko-tls
sparko-host=0.0.0.0
sparko-port=9737
sparko-login=mywalletusername:mywalletpassword
sparko-keys=secretaccesskeythatcanreadstuff:+listchannels,+listnodes;ultrasecretaccesskeythatcansendandreceive:+invoice,+listinvoices,+delinvoice,+decodepay,+waitpay
```

To use TLS (`https://`), generate your keys first:

```
cd ~/.lightning/sparko-tls/
openssl genrsa -out key.pem 2048
openssl req -new -x509 -sha256 -key key.pem -out cert.pem -days 3650
```
