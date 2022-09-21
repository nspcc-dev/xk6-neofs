import datagen from 'k6/x/neofs/datagen';
import native from 'k6/x/neofs/native';
import registry from 'k6/x/neofs/registry';
import { SharedArray } from 'k6/data';
import { sleep } from 'k6';

const obj_list = new SharedArray('obj_list', function () {
    return JSON.parse(open(__ENV.PREGEN_JSON)).objects;
});

const container_list = new SharedArray('container_list', function () {
    return JSON.parse(open(__ENV.PREGEN_JSON)).containers;
});

const read_size = JSON.parse(open(__ENV.PREGEN_JSON)).obj_size;

/* 
    ./k6 run -e PROFILE=0:60 -e CLIENTS=200 -e WRITE_OBJ_SIZE=1024 \
      -e GRPC_ENDPOINTS=host1:8080,host2:8080 \
      -e PREGEN_JSON=test.json \
      scenarios/grpc.js

    REGISTRY_FILE - if set, all produced objects will be stored in database for subsequent verification.
*/

// Parse profile from env (format is write:duration)
//   * write    - percent of VUs performing write operations (the rest will be read VUs)
//   * duration - duration in seconds
const [ write, duration ] = __ENV.PROFILE.split(':');

// Allocate VUs between write and read operations
const read_vu_count = Math.ceil(__ENV.CLIENTS / 100 * (100 - parseInt(write)));
const write_vu_count = __ENV.CLIENTS - read_vu_count;

// Select random gRPC endpoint for current VU
const grpc_endpoints = __ENV.GRPC_ENDPOINTS.split(',');
const grpc_endpoint = grpc_endpoints[Math.floor(Math.random() * grpc_endpoints.length)];
const grpc_client = native.connect(grpc_endpoint, '');

const registry_enabled = !!__ENV.REGISTRY_FILE;
const obj_registry = registry_enabled ? registry.open(__ENV.REGISTRY_FILE) : undefined;

const generator = datagen.generator(1024 * parseInt(__ENV.WRITE_OBJ_SIZE));

const scenarios = {};

if (write_vu_count > 0) {
    scenarios.write = {
        executor: 'constant-vus',
        vus: write_vu_count,
        duration: `${duration}s`,
        exec: 'obj_write', 
        gracefulStop: '5s',
    }
}

if (read_vu_count > 0) {
    scenarios.read = {
        executor: 'constant-vus',
        vus: read_vu_count,
        duration: `${duration}s`,
        exec: 'obj_read', 
        gracefulStop: '5s',
    }
}

export function setup() {
    console.log("Pregenerated containers: " + container_list.length);
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

    const headers = {
        unique_header: uuidv4()
    };
    const container = container_list[Math.floor(Math.random() * container_list.length)];

    const { payload, hash } = generator.genPayload(registry_enabled);
    const resp = grpc_client.put(container, headers, payload);
    if (!resp.success) {
        console.log(resp.error);
        return;
    }

    if (obj_registry) {
        obj_registry.addObject(container, resp.object_id, "", "", hash);
    }
}

export function obj_read() {
    if (__ENV.SLEEP) {
        sleep(__ENV.SLEEP);
    }

    const obj = obj_list[Math.floor(Math.random() * obj_list.length)];
    const resp = grpc_client.get(obj.container, obj.object)
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
