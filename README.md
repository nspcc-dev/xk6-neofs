# xk6-neofs

This is a [k6](https://go.k6.io/k6) extension using the 
[xk6](https://github.com/k6io/xk6) system, that allows to test 
[NeoFS](https://github.com/nspcc-dev/neofs-node) related protocols.

## Build

To build a `k6` binary with this extension, first ensure you have the prerequisites:

- Go
- Git

1. Install `xk6` framework for extending `k6`:
```shell
go install go.k6.io/xk6/cmd/xk6@latest
```

2. Clone this repository
```shell
git clone github.com/nspcc-dev/xk6-neofs
cd xk6-neofs
```

3. Build the binary:
```shell
xk6 build --with github.com/nspcc-dev/xk6-neofs=.
```

4. Run k6:
```shell
./k6 run test-script.js
```

## API

### Native

Create native client with `connect` method. Arguments:
- neofs storage node endpoint
- WIF (empty value produces random key)

```js
import native from 'k6/x/neofs/native';
const neofs_cli = native.connect("s01.neofs.devenv:8080", "")
```

#### Methods
- `setBufferSize(size)`. Sets internal buffer size for data upload and 
  download. Default is 64 KiB.
- `put(container_id, headers, payload)`. Returns dictionary with `success` 
  boolean flag, `object_id` string, and `error` string.
- `get(container_id, object_id)`. Returns dictionary with `success` boolean
  flag, and `error` string.
- `onsite(container_id, payload)`. Returns NeoFS object instance with prepared
  headers. Invoke `put(headers)` method on this object to upload it into NeoFS.
  It returns dicrionary with `success` boolean flag, `object_id` string and
  `error` string.

### S3

Create s3 client with `connect` method. Arguments:
- s3 gateway endpoint

Credentials are taken from default AWS configuration files and ENVs.

```js
import s3 from 'k6/x/neofs/s3';
const s3_cli = s3.connect("http://s3.neofs.devenv:8080")
```

#### Methods
- `put(bucket, key, payload)`. Returns dictionary with `success` boolean flag 
  and `error` string.
- `get(bucket, key)`. Returns dictionary with `success` boolean flag and `error`
  string.

## Examples

See native protocol and s3 test suit examples in [examples](./examples) dir.