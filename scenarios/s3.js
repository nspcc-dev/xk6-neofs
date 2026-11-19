import datagen from 'k6/x/neofs/datagen';
import registry from 'k6/x/neofs/registry';
import s3 from 'k6/x/neofs/s3';
import { SharedArray } from 'k6/data';
import { sleep } from 'k6';

const object_list = new SharedArray('obj_list', function () {
    return JSON.parse(open(__ENV.PREGEN_JSON)).objects;
});

const bucket_list = new SharedArray('bucket_list', function () {
    return JSON.parse(open(__ENV.PREGEN_JSON)).buckets;
});

const obj_list_by_bucket = {};
for (const obj of object_list) {
    if (!obj_list_by_bucket[obj.bucket]) {
        obj_list_by_bucket[obj.bucket] = [];
    }

    obj_list_by_bucket[obj.bucket].push(obj);
}

const read_size = JSON.parse(open(__ENV.PREGEN_JSON)).obj_size;

const s3_endpoints = __ENV.S3_ENDPOINTS.split(',');
const endpoint_selection = __ENV.ENDPOINT_SELECTION || 'random';
if (endpoint_selection !== 'random' && endpoint_selection !== 'round-robin') {
    throw new Error(`invalid ENDPOINT_SELECTION '${endpoint_selection}', expected 'random' or 'round-robin'`);
}
let s3_endpoint;
if (endpoint_selection === 'random') {
    s3_endpoint = s3_endpoints[Math.floor(Math.random() * s3_endpoints.length)];
}

if (endpoint_selection === 'round-robin') {
    const vuIndex = __VU > 0 ? __VU - 1 : 0;
    s3_endpoint = s3_endpoints[vuIndex % s3_endpoints.length];
}

const bucket_selection = __ENV.BUCKET_SELECTION || 'random';
if (bucket_selection !== 'random' && bucket_selection !== 'round-robin') {
    throw new Error(`invalid BUCKET_SELECTION '${bucket_selection}', expected 'random' or 'round-robin'`);
}
let bucket;
if (bucket_selection === 'round-robin') {
    const vuIndex = __VU > 0 ? __VU - 1 : 0;
    bucket = bucket_list[vuIndex % bucket_list.length];
}

if (__VU > 0) {
    console.log(`VU ID: ${__VU}, Bucket: ${bucket || 'random for each request'}, Endpoint: ${s3_endpoint}`);
}

const s3_client = s3.connect(`http://${s3_endpoint}`);

const registry_enabled = !!__ENV.REGISTRY_FILE;
const obj_registry = registry_enabled ? registry.open(__ENV.REGISTRY_FILE) : undefined;

const duration = __ENV.DURATION;
const list_prefix = __ENV.LIST_PREFIX || '';

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

const stat_vu_count = parseInt(__ENV.STATERS || '0');
if (stat_vu_count > 0) {
    scenarios.stat = {
        executor: 'constant-vus',
        vus: stat_vu_count,
        duration: `${duration}s`,
        exec: 'obj_stat',
        gracefulStop: '5s',
    };
}

const list_vu_count = parseInt(__ENV.LISTERS || '0');
if (list_vu_count > 0) {
    scenarios.list = {
        executor: 'constant-vus',
        vus: list_vu_count,
        duration: `${duration}s`,
        exec: 'obj_list',
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
    summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

export function setup() {
    const total_vu_count = write_vu_count + read_vu_count + stat_vu_count + list_vu_count + delete_vu_count;

    console.log(`Pregenerated buckets:          ${bucket_list.length}`);
    console.log(`Pregenerated read object size: ${read_size}`);
    console.log(`Pregenerated total objects:    ${object_list.length}`);
    console.log(`Reading VUs:                   ${read_vu_count}`);
    console.log(`Statting VUs:                  ${stat_vu_count}`);
    console.log(`Listing VUs:                   ${list_vu_count}`);
    console.log(`Writing VUs:                   ${write_vu_count}`);
    console.log(`Deleting VUs:                  ${delete_vu_count}`);
    console.log(`Total VUs:                     ${total_vu_count}`);
}

export function teardown(data) {
    if (obj_registry) {
        obj_registry.close();
    }
}

function select_bucket() {
    return bucket || bucket_list[Math.floor(Math.random() * bucket_list.length)];
}

export function obj_write() {
    if (__ENV.SLEEP_WRITE) {
        sleep(__ENV.SLEEP_WRITE);
    }

    const key = __ENV.OBJ_NAME || uuidv4();
    const vu_bucket = select_bucket();

    const { payload, hash } = generator.genPayload(registry_enabled);
    const resp = s3_client.put(vu_bucket, key, payload);
    if (!resp.success) {
        console.log(resp.error);
        return;
    }

    if (obj_registry) {
        obj_registry.addObject("", "", vu_bucket, key, hash);
    }
}

function select_object() {
    const vu_bucket = select_bucket();

    const selected_obj_list = obj_list_by_bucket[vu_bucket] || object_list;
    return selected_obj_list[Math.floor(Math.random() * selected_obj_list.length)] || object_list[Math.floor(Math.random() * object_list.length)];
}

export function obj_read() {
    if (__ENV.SLEEP_READ) {
        sleep(__ENV.SLEEP_READ);
    }

    const obj = select_object();
    if (!obj) {
        return;
    }

    const resp = s3_client.get(obj.bucket, obj.object);
    if (!resp.success) {
        console.log(resp.error);
    } 
}

export function obj_stat() {
    if (__ENV.SLEEP_STAT) {
        sleep(__ENV.SLEEP_STAT);
    }

    const obj = select_object();
    if (!obj) {
        return;
    }

    const resp = s3_client.head(obj.bucket, obj.object);
    if (!resp.success) {
        console.log(resp.error);
    }
}

export function obj_list() {
    if (__ENV.SLEEP_LIST) {
        sleep(__ENV.SLEEP_LIST);
    }

    const vu_bucket = select_bucket();
    const resp = s3_client.list(vu_bucket, list_prefix);
    if (!resp.success) {
        console.log(resp.error);
    }
}

export function obj_delete() {
    if (__ENV.SLEEP_DELETE) {
        sleep(__ENV.SLEEP_DELETE);
    }

    const obj = obj_to_delete_selector.nextObject();
    if (!obj) {
        return;
    }

    const resp = s3_client.delete(obj.s3_bucket, obj.s3_key);
    if (!resp.success) {
        console.log(`Error deleting object ${obj.id}: ${resp.error}`);
        return;
    }

    obj_registry.deleteObject(obj.id);
}

export function uuidv4() {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
        let r = Math.random() * 16 | 0, v = c === 'x' ? r : (r & 0x3 | 0x8);
        return v.toString(16);
    });
}
