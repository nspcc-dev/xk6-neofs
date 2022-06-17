---

# How to execute scenarios

## gRPC

1. Create container:

```shell
$ neofs-cli --rpc-endpoint node1.intra:8080 -w wallet.json container create --policy "REP 1 IN X CBF 1 SELECT 1 FROM * AS X" --basic-acl public-read-write --await
```

2. Execute scenario with options:

```shell
$ k6 run -e CID=GTiJKAntLNchKeGDHsJhda8LhChnNoPD1nfQC3NUuV2J -e PROFILE=70:1024:10 -e CLIENTS=100 -e PRELOAD_OBJ=100  grpc.js
```

Options:
  * CID - container ID
  * PROFILE - format write:obj_size:duration
      * write    - write operations in percent, relative to read operations
      * obj_size - size of objects in kilobytes
      * duration - time in sec
  * CLIENTS - number of VUs for all operations
  * PRELOAD_OBJ - objects will be uploaded to the container in the Setup step and will be used for read operations

## S3

1. Create s3 credential:

```shell
$ neofs-s3-authmate issue-secret --wallet wallet.json --peer node1.intra:8080 --gate-public-key 03d33a2cc7b8daaa5a3df3fccf065f7cf1fc6a3279efc161fcec512dcc0c1b2277 --gate-public-key 03ff0ad212e10683234442530bfd71d0bb18c3fbd6459aba768eacf158b0c359a2 --gate-public-key 033ae03ff30ed3b6665af69955562cfc0eae18d50e798ab31f054ee22e32fee993 --gate-public-key 02127c7498de0765d2461577c9d4f13f916eefd1884896183e6de0d9a85d17f2fb --bearer-rules rules.json  --container-placement-policy "REP 1 IN X CBF 1 SELECT 1 FROM * AS X"

Enter password for wallet.json > 
{
  "access_key_id": "38xRsCTb2LTeCWNK1x5dPYeWC1X22Lq4ahKkj1NV6tPk0Dack8FteJHQaW4jkGWoQBGQ8R8UW6CdoAr7oiwS7fFQb",
  "secret_access_key": "e671e353375030da3fbf521028cb43810280b814f97c35672484e303037ea1ab",
  "owner_private_key": "48e83ab313ca45fe73c7489565d55652a822ef659c75eaba2d912449713f8e58",
  "container_id": "38xRsCTb2LTeCWNK1x5dPYeWC1X22Lq4ahKkj1NV6tPk"
}
```

Run `aws configure`.

2. Create bucket:

```shell
$ aws --no-verify-ssl s3api create-bucket --bucket cats-24 --endpoint http://node1.neofs:8084

```

3. Execute scenario with options:

```shell
$ k6 run -e BUCKET=cats -e PROFILE=70:1024:10 -e CLIENTS=100 -e PRELOAD_OBJ=100 s3.js

```

## HTTP

1. Execute scenario with options:

```shell
$ k6 run -e CID=GTiJKAntLNchKeGDHsJhda8LhChnNoPD1nfQC3NUuV2J -e PROFILE=70:1024:10 -e CLIENTS=100 -e PRELOAD_OBJ=100  http.js
```
