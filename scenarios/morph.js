import datagen from 'k6/x/neofs/datagen';
import registry from 'k6/x/neofs/registry';
import http from 'k6/http';
import encoding from 'k6/encoding';
import { SharedArray } from 'k6/data';
import { Counter, Trend } from 'k6/metrics';
import { sleep } from 'k6';

// Pregenerated buckets/objects, produced by `scenarios/preset/preset_morph.py`.
// JSON shape matches the one used by `scenarios/s3.js`:
//   { "buckets": [ "name1", ... ], "objects": [ { "bucket": ..., "object": ... }, ... ], "obj_size": "<n> Kb" }
const obj_list = new SharedArray('obj_list', function () {
    return JSON.parse(open(__ENV.PREGEN_JSON)).objects;
});

const bucket_list = new SharedArray('bucket_list', function () {
    return JSON.parse(open(__ENV.PREGEN_JSON)).buckets;
});

const read_size = JSON.parse(open(__ENV.PREGEN_JSON)).obj_size;

// Pick a Morph endpoint for this VU. Endpoints are full base URLs like
// "http://host:8080" (no trailing slash required).
const morph_endpoints = __ENV.MORPH_ENDPOINTS.split(',');
const endpoint_to_use = __VU % morph_endpoints.length;
const morph_endpoint = morph_endpoints[endpoint_to_use].replace(/\/+$/, '');
console.log(`VU ID: ${__VU}, Morph endpoint in use: ${morph_endpoint}`);

const auth_token = __ENV.MORPH_AUTH_TOKEN;
if (!auth_token) {
    throw new Error('MORPH_AUTH_TOKEN env var is required');
}
const auth_header = `Bearer ${auth_token}`;

const registry_enabled = !!__ENV.REGISTRY_FILE;
const obj_registry = registry_enabled ? registry.open(__ENV.REGISTRY_FILE) : undefined;

const duration = __ENV.DURATION;

const delete_age = __ENV.DELETE_AGE ? parseInt(__ENV.DELETE_AGE) : undefined;
let obj_to_delete_selector = undefined;
if (registry_enabled && delete_age) {
    obj_to_delete_selector = registry.getSelector(
        __ENV.REGISTRY_FILE,
        "obj_to_delete",
        __ENV.SELECTION_SIZE ? parseInt(__ENV.SELECTION_SIZE) : 0,
        {
            status: "created",
            age: delete_age,
        }
    );
}

const generator = datagen.generator(1024 * parseInt(__ENV.WRITE_OBJ_SIZE));

// Per-operation metrics. Names mirror the native/s3 drivers' style so dashboards
// can be reused.
const obj_put_total = new Counter('morph_obj_put_total');
const obj_put_fails = new Counter('morph_obj_put_fails');
const obj_put_duration = new Trend('morph_obj_put_duration', true);

const obj_get_total = new Counter('morph_obj_get_total');
const obj_get_fails = new Counter('morph_obj_get_fails');
const obj_get_duration = new Trend('morph_obj_get_duration', true);

const obj_delete_total = new Counter('morph_obj_delete_total');
const obj_delete_fails = new Counter('morph_obj_delete_fails');
const obj_delete_duration = new Trend('morph_obj_delete_duration', true);

const scenarios = {};

const write_vu_count = parseInt(__ENV.WRITERS || '0');
if (write_vu_count > 0) {
    scenarios.write = {
        executor: 'constant-vus',
        vus: write_vu_count,
        duration: `${duration}s`,
        exec: 'obj_write',
        gracefulStop: '5s',
    };
}

const read_vu_count = parseInt(__ENV.READERS || '0');
if (read_vu_count > 0) {
    scenarios.read = {
        executor: 'constant-vus',
        vus: read_vu_count,
        duration: `${duration}s`,
        exec: 'obj_read',
        gracefulStop: '5s',
    };
}

const delete_vu_count = parseInt(__ENV.DELETERS || '0');
if (delete_vu_count > 0) {
    if (!obj_to_delete_selector) {
        throw 'Positive DELETE worker number without a proper object selector';
    }

    scenarios.delete = {
        executor: 'constant-vus',
        vus: delete_vu_count,
        duration: `${duration}s`,
        exec: 'obj_delete',
        gracefulStop: '5s',
    };
}

export const options = {
    scenarios,
    setupTimeout: '5s',
};

export function setup() {
    const total_vu_count = write_vu_count + read_vu_count + delete_vu_count;

    console.log(`Pregenerated buckets:          ${bucket_list.length}`);
    console.log(`Pregenerated read object size: ${read_size}`);
    console.log(`Pregenerated total objects:    ${obj_list.length}`);
    console.log(`Reading VUs:                   ${read_vu_count}`);
    console.log(`Writing VUs:                   ${write_vu_count}`);
    console.log(`Deleting VUs:                  ${delete_vu_count}`);
    console.log(`Total VUs:                     ${total_vu_count}`);
}

export function teardown(data) {
    if (obj_registry) {
        obj_registry.close();
    }
}

export function obj_write() {
    if (__ENV.SLEEP_WRITE) {
        sleep(__ENV.SLEEP_WRITE);
    }

    const key = __ENV.OBJ_NAME || uuidv4();
    const bucket = bucket_list[Math.floor(Math.random() * bucket_list.length)];

    const { payload, hash } = generator.genPayload(registry_enabled);

    const headers = {
        Authorization: auth_header,
        'Content-Type': 'application/octet-stream',
        // Morph expects the object path to be passed via the `x-morph-path`
        // header, base64-encoded (see console/pytest_tests/lib/rest.py).
        'x-morph-path': encoding.b64encode(key),
    };

    obj_put_total.add(1);
    const start = Date.now();
    const resp = http.post(
        `${morph_endpoint}/api/v1/buckets/${bucket}/objects`,
        payload,
        { headers, tags: { op: 'morph_put' } },
    );
    if (resp.status < 200 || resp.status >= 300) {
        obj_put_fails.add(1);
        console.log(`PUT ${bucket}/${key} failed: ${resp.status} ${resp.body}`);
        return;
    }
    obj_put_duration.add(Date.now() - start);

    if (obj_registry) {
        // Morph objects are stored in the registry using the s3_bucket/s3_key
        // slots since the semantics (bucket + string key + payload hash) match.
        // `verify.js` checks `MORPH_ENDPOINTS` to route these rows to morph
        // verification instead of s3.
        obj_registry.addObject("", "", bucket, key, hash);
    }
}

export function obj_read() {
    if (__ENV.SLEEP_READ) {
        sleep(__ENV.SLEEP_READ);
    }

    const obj = obj_list[Math.floor(Math.random() * obj_list.length)];
    const url = `${morph_endpoint}/api/v1/buckets/${obj.bucket}/objects/${encodeURIComponent(obj.object)}`;

    obj_get_total.add(1);
    const start = Date.now();
    const resp = http.get(url, {
        headers: { Authorization: auth_header },
        responseType: 'binary',
        tags: { op: 'morph_get' },
    });
    if (resp.status !== 200) {
        obj_get_fails.add(1);
        console.log(`GET ${obj.bucket}/${obj.object} failed: ${resp.status}`);
        return;
    }
    obj_get_duration.add(Date.now() - start);
}

export function obj_delete() {
    if (__ENV.SLEEP_DELETE) {
        sleep(__ENV.SLEEP_DELETE);
    }

    const obj = obj_to_delete_selector.nextObject();
    if (!obj) {
        return;
    }

    const url = `${morph_endpoint}/api/v1/buckets/${obj.s3_bucket}/objects/${encodeURIComponent(obj.s3_key)}`;

    obj_delete_total.add(1);
    const start = Date.now();
    const resp = http.del(url, null, {
        headers: { Authorization: auth_header },
        tags: { op: 'morph_delete' },
    });
    if (resp.status < 200 || resp.status >= 300) {
        obj_delete_fails.add(1);
        console.log(`DELETE ${obj.s3_bucket}/${obj.s3_key} failed: ${resp.status}`);
        return;
    }
    obj_delete_duration.add(Date.now() - start);

    obj_registry.deleteObject(obj.id);
}

export function uuidv4() {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function (c) {
        let r = Math.random() * 16 | 0, v = c === 'x' ? r : (r & 0x3 | 0x8);
        return v.toString(16);
    });
}
