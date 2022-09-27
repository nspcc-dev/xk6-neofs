---

# How to execute scenarios

## Common options for gRPC, HTTP, S3 scenarios:

Scenarios `grpc.js`, `http.js` and `s3.js` support the following options:
  * `DURATION` - duration of scenario in seconds.
  * `READERS` - number of VUs performing read operations.
  * `WRITERS` - number of VUs performing write operations.
  * `REGISTRY_FILE` - if set, all produced objects will be stored in database for subsequent verification. Database file name will be set to the value of `REGISTRY_FILE`.
  * `WRITE_OBJ_SIZE` - object size in kb for write(PUT) operations.
  * `PREGEN_JSON` - path to json file with pre-generated containers and objects (in case of http scenario we use json pre-generated for grpc scenario).
  * `SLEEP` - time interval (in seconds) between VU iterations.

Examples of how to use these options are provided below for each scenario.

## gRPC

1. Create pre-generated containers or objects:

The tests will use all pre-created containers for PUT operations and all pre-created objects for READ operations.

```shell
$ ./scenarios/preset/preset_grpc.py --size 1024 --containers 1 --out grpc.json --endpoint host1:8080 --preload_obj 500
```

2. Execute scenario with options:

```shell
$ ./k6 run -e DURATION=60 -e WRITE_OBJ_SIZE=8192 -e READERS=20 -e WRITERS=20 -e DELETERS=30 -e DELETE_AGE=10 -e REGITRY_FILE=registry.bolt -e GRPC_ENDPOINTS=host1:8080,host2:8080 -e PREGEN_JSON=./grpc.json scenarios/grpc.js
```

Options (in addition to the common options):
  * `GRPC_ENDPOINTS` - GRPC endpoints of neoFS in format `host:port`. To specify multiple endpoints separate them by comma.
  * `DELETERS` - number of VUs performing delete operations (using deleters requires that options `DELETE_AGE` and `REGISTRY_FILE` are specified as well).
  * `DELETE_AGE` - age of object in seconds before which it can not be deleted. This parameter can be used to control how many objects we have in the system under load.

## HTTP

1. Create pre-generated containers or objects:

There is no dedicated script to preset HTTP scenario, so we use the same script as for gRPC:
```shell
$ ./scenarios/preset/preset_grpc.py --size 1024 --containers 1 --out grpc.json --endpoint host1:8080 --preload_obj 500
```

2. Execute scenario with options:

```shell
$ ./k6 run -e DURATION=60 -e WRITE_OBJ_SIZE=8192 -e READERS=10 -e WRITERS=20 -e REGITRY_FILE=registry.bolt -e HTTP_ENDPOINTS=host1:8888,host2:8888 -e PREGEN_JSON=./grpc.json scenarios/http.js
```

Options (in addition to the common options):
  * `HTTP_ENDPOINTS` - endpoints of HTTP gateways in format `host:port`. To specify multiple endpoints separate them by comma.

## S3

1. Create s3 credentials:

```shell
$ neofs-s3-authmate issue-secret --wallet wallet.json --peer host1:8080 --gate-public-key 03d33a2cc7b8daaa5a3df3fccf065f7cf1fc6a3279efc161fcec512dcc0c1b2277 --gate-public-key 03ff0ad212e10683234442530bfd71d0bb18c3fbd6459aba768eacf158b0c359a2 --gate-public-key 033ae03ff30ed3b6665af69955562cfc0eae18d50e798ab31f054ee22e32fee993 --gate-public-key 02127c7498de0765d2461577c9d4f13f916eefd1884896183e6de0d9a85d17f2fb --bearer-rules rules.json  --container-placement-policy "REP 1 IN X CBF 1 SELECT 1 FROM * AS X"

Enter password for wallet.json > 
{
  "access_key_id": "38xRsCTb2LTeCWNK1x5dPYeWC1X22Lq4ahKkj1NV6tPk0Dack8FteJHQaW4jkGWoQBGQ8R8UW6CdoAr7oiwS7fFQb",
  "secret_access_key": "e671e353375030da3fbf521028cb43810280b814f97c35672484e303037ea1ab",
  "owner_private_key": "48e83ab313ca45fe73c7489565d55652a822ef659c75eaba2d912449713f8e58",
  "container_id": "38xRsCTb2LTeCWNK1x5dPYeWC1X22Lq4ahKkj1NV6tPk"
}
```

Run `aws configure`.

2. Create pre-generated buckets or objects:

The tests will use all pre-created buckets for PUT operations and all pre-created objects for READ operations.

```shell
$ ./scenarios/preset/preset_s3.py --size 1024 --buckets 1 --out s3.json --endpoint host1:8084 --preload_obj 500
```

3. Execute scenario with options:

```shell
$ ./k6 run -e DURATION=60 -e WRITE_OBJ_SIZE=8192 -e READERS=20 -e WRITERS=20 -e DELETERS=30 -e DELETE_AGE=10 -e S3_ENDPOINTS=host1:8084,host2:8084 -e PREGEN_JSON=s3.json scenarios/s3.js
```

Options (in addition to the common options):
  * `S3_ENDPOINTS` - endpoints of S3 gateways in format `host:port`. To specify multiple endpoints separate them by comma.
  * `DELETERS` - number of VUs performing delete operations (using deleters requires that options `DELETE_AGE` and `REGISTRY_FILE` are specified as well).
  * `DELETE_AGE` - age of object in seconds before which it can not be deleted. This parameter can be used to control how many objects we have in the system under load.
  * `OBJ_NAME` - if specified, this name will be used for all write operations instead of random generation.

## Verify

This scenario allows to verify that objects created by a previous run are really stored in the system and their data is not corrupted. Running this scenario assumes that you've already run gRPC or HTTP or S3 scenario with option `REGISTRY_FILE`.

To verify stored objects execute scenario with options:

```
./k6 run -e CLIENTS=200 -e TIME_LIMIT=120 -e GRPC_ENDPOINTS=host1:8080,host2:8080 -e S3_ENDPOINTS=host1:8084,host2:8084 -e REGISTRY_FILE=registry.bolt scenarios/verify.js
```

Scenario picks up all objects in `created` status. If object is stored correctly, its' status will be changed into `verified`. If object does not exist or its' data is corrupted, then the status will be changed into `invalid`.
Scenario ends as soon as all objects are checked or time limit is exceeded.

Objects produced by HTTP scenario will be verified via gRPC endpoints.

Options:
  * `CLIENTS` - number of VUs for verifying objects (VU can handle both GRPC and S3 objects)
  * `TIME_LIMIT` - amount of time in seconds that is sufficient to verify all objects. If this time interval ends, then verification process will be interrupted and objects that have not been checked will stay in the `created` state.
  * `REGISTRY_FILE` - database file from which objects for verification should be read.
  * `SLEEP` - time interval (in seconds) between VU iterations.
