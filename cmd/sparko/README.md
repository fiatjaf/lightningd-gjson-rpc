## The `sparko` plugin.

The famous [Spark wallet](https://github.com/shesek/spark-wallet) repackaged as a single-binary plugin.

This works either as a personal wallet with a nice UI (see link above) or as a full-blown HTTP-RPC bridge to your node that can be used to develop apps. In conjunction with the [`webhook` plugin](https://github.com/fiatjaf/lightningd-gjson-rpc/tree/master/cmd/webhook) it becomes even more powerful.

It has some differences (advantages?) over the original Spark wallet:

* Single binary: No dependencies to manage, just grab the manage and throw it in your `lightningd` plugins folder.
* Runs as a plugin: this means you don't have to manage the server, it will be managed by `lightningd` and will always be running as long as your node is running.
* Multiple keys with fine-grained permissions: create keys that can only call some methods.
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

```shell
sparko-host=0.0.0.0
sparko-port=9737

# the tls path is just the directory where your self-signed key and certificate are.
# (see below for code snippets that generate them on Linux)
# the path is relative to your lightning-dir, so "sparko-tls" will translate to "~/.lightning/sparko-tls/"
# if not specified the app will run without TLS (as http://)
sparko-tls-path=sparko-tls

# login credentials for using the wallet app.
# under the hood these are translated into an access key with full access.
# the default login is none, which doesn't allow you to use the wallet app,
#   but you can still use the /rpc endpoint with other keys specified at sparko-keys=
sparko-login=mywalletusername:mywalletpassword

# a list of semicolon-separated pairs of keys:methodsallowed
# the syntax is a little confusing at first sight, but actually simple: just list the methods after the key
#   if you prefix the methods with a + (plus) means they are whitelisted and all others blacklisted,
#   prefixing them with a - (minus) means they are blacklisted and all other whitelisted for that key.
#   not listing any method means the given key has full access to all methods..
# (keys must be secret and random)
sparko-keys=masterkeythatcandoeverything;secretaccesskeythatcanreadstuff:+listchannels,+listnodes;ultrasecretaccesskeythatcansendandreceive:+invoice,+listinvoices,+delinvoice,+decodepay,+waitpay,+waitinvoice
# for the example above the initialization logs (mixed with lightningd logs) should print something like
2019/09/27 00:48:46 plugin-sparko Keys read: masterkeythatcandoeverything (full-access), secretaccesskeythatcanreadstuff (2 whitelisted), ultrasecretaccesskeythatcansendandreceive (6 whitelisted)
```

To use TLS with a self-signed certificate (`https://`), generate your certificate first:

```
cd ~/.lightning/sparko-tls/
openssl genrsa -out key.pem 2048
openssl req -new -x509 -sha256 -key key.pem -out cert.pem -days 3650
```

To use a certificate signed by LetsEncrypt, you must be able to bind to ports 80 and 443, which generally requires running as root. Specify options like the following:

```shell
sparko-host=sparko.mydomain.com
sparko-tls-path=sparko-letsencrypt
sparko-letsencrypt-email=myemail@gmail.com
```

Then try to visit `http://sparko.mydomain.com/`. If all is well you should get redirected to the `https://` page, if something is wrong it should appear on the logs.

### Errors

When starting `lightningd`, check the logs for errors regarding `sparko` initialization, they will be prefixed with `"plugin-sparko"`.

### Call the HTTP RPC

Replace the following with your actual values:

```
curl -k https://0.0.0.0:9737/rpc -d '{"method": "pay", "params": ["lnbc..."]}' -H 'X-Access masterkeythatcandoeverything'
```

### Open the wallet UI

Visit `https://0.0.0.0:9737/` (replacing with your actual values).
