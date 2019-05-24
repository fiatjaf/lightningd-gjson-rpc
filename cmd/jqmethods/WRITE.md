## _jq methods_: how to make my own

All methods are just YAML files.

The file name is the method name.

See https://github.com/fiatjaf/jqmethods for a couple of examples.

## Available ways of writing _jq method_:

### Version 0

The most basic method takes the following form:

```yaml
rpc: <the lightningd RPC method that will be called>
filter: <the jq filter that will be applied>
description: <a brief description, optional>
```

For example, a _jq method_ that shows your lightning node id + address could be something like the following:

```yaml
rpc: getinfo
description: Shows an address I can tell people to connect to.
filter: | # this means any indented line from here will be treated as the string value of "filter"
  .id + "@" +
    .address[0].address + ":" +
    (.address[0].port | tostring)
```
