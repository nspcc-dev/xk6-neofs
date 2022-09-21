import datagen from 'k6/x/neofs/datagen';
import registry from 'k6/x/neofs/registry';
import s3 from 'k6/x/neofs/s3';
import { SharedArray } from 'k6/data';
import { sleep } from 'k6';

const obj_list = new SharedArray('obj_list', function () {
    return JSON.parse(open(__ENV.PREGEN_JSON)).objects;
});

const bucket_list = new SharedArray('bucket_list', function () {
    return JSON.parse(open(__ENV.PREGEN_JSON)).buckets;
});

const read_size = JSON.parse(open(__ENV.PREGEN_JSON)).obj_size;

/* 
    ./k6 run -e PROFILE=0:60 -e CLIENTS=200 -e WRITE_OBJ_SIZE=1024 \
      -e S3_ENDPOINTS=host1:8084,host2:8084 -e PREGEN_JSON=test.json \
      scenarios/s3.js

    OBJ_NAME - if specified, this name will be used for all write operations instead of random generation.
    REGISTRY_FILE - if set, all produced objects will be stored in database for subsequent verification.
*/

// Parse profile from env
const [ write, duration ] = __ENV.PROFILE.split(':');

// Allocate VUs between write and read operations
let read_vu_count = Math.ceil(__ENV.CLIENTS / 100 * (100 - parseInt(write)));
let write_vu_count = __ENV.CLIENTS - read_vu_count;

// Select random S3 endpoint for current VU
const s3_endpoints = __ENV.S3_ENDPOINTS.split(',');
const s3_endpoint = s3_endpoints[Math.floor(Math.random() * s3_endpoints.length)];
const s3_client = s3.connect(`http://${s3_endpoint}`);

const registry_enabled = !!__ENV.REGISTRY_FILE;
const obj_registry = registry_enabled ? registry.open(__ENV.REGISTRY_FILE) : undefined;

const generator = datagen.generator(1024 * parseInt(__ENV.WRITE_OBJ_SIZE));

const scenarios = {};

if (write_vu_count > 0){
    scenarios.write = {
        executor: 'constant-vus',
        vus: write_vu_count,
        duration: `${duration}s`,
        exec: 'obj_write', 
        gracefulStop: '5s',
    };
}

if (read_vu_count > 0){
    scenarios.read = {
        executor: 'constant-vus',
        vus: read_vu_count,
        duration: `${duration}s`,
        exec: 'obj_read', 
        gracefulStop: '5s',
    };
}

export function setup() {
    console.log("Pregenerated buckets: " + bucket_list.length);
    console.log("Pregenerated read object size: " + read_size);
    console.log("Pregenerated total objects: " + obj_list.length);
}

export function teardown(data) {
    if (obj_registry) {
        obj_registry.close();
    }
}

export const options = {
    scenarios,
    setupTimeout: '5s',
};

export function obj_write() {
    if (__ENV.SLEEP) {
        sleep(__ENV.SLEEP);
    }

    const key = __ENV.OBJ_NAME || uuidv4();
    const bucket = bucket_list[Math.floor(Math.random() * bucket_list.length)];

    const { payload, hash } = generator.genPayload(registry_enabled);
    const resp = s3_client.put(bucket, key, payload);
    if (!resp.success) {
        console.log(resp.error);
        return;
    }

    if (obj_registry) {
        obj_registry.addObject("", "", bucket, key, hash);
    }
}

export function obj_read() {
    if (__ENV.SLEEP) {
        sleep(__ENV.SLEEP);
    }

    const obj = obj_list[Math.floor(Math.random() * obj_list.length)];

    const resp = s3_client.get(obj.bucket, obj.object);
    if (!resp.success) {
        console.log(resp.error);
    } 
}

export function uuidv4() {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
        let r = Math.random() * 16 | 0, v = c === 'x' ? r : (r & 0x3 | 0x8);
        return v.toString(16);
    });
}
