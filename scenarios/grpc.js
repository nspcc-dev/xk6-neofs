import datagen from 'k6/x/neofs/datagen';
import native from 'k6/x/neofs/native';
import registry from 'k6/x/neofs/registry';
import { SharedArray } from 'k6/data';
import { sleep } from 'k6';

const object_list = new SharedArray('obj_list', function () {
    return JSON.parse(open(__ENV.PREGEN_JSON)).objects;
});

const container_list = new SharedArray('container_list', function () {
    return JSON.parse(open(__ENV.PREGEN_JSON)).containers;
});

const obj_list_by_container = {};
for (const obj of object_list) {
    if (!obj_list_by_container[obj.container]) {
        obj_list_by_container[obj.container] = [];
    }

    obj_list_by_container[obj.container].push(obj);
}

const read_size = JSON.parse(open(__ENV.PREGEN_JSON)).obj_size;

const grpc_endpoints = __ENV.GRPC_ENDPOINTS.split(',');
const endpoint_selection = __ENV.ENDPOINT_SELECTION || 'random';
if (endpoint_selection !== 'random' && endpoint_selection !== 'round-robin') {
    throw new Error(`invalid ENDPOINT_SELECTION '${endpoint_selection}', expected 'random' or 'round-robin'`);
}
let grpc_endpoint;
if (endpoint_selection === 'round-robin') {
    const vuIndex = __VU > 0 ? __VU - 1 : 0;
    grpc_endpoint = grpc_endpoints[vuIndex % grpc_endpoints.length];
}

const bucket_selection = __ENV.BUCKET_SELECTION || 'random';
if (bucket_selection !== 'random' && bucket_selection !== 'round-robin') {
    throw new Error(`invalid BUCKET_SELECTION '${bucket_selection}', expected 'random' or 'round-robin'`);
}
let container;
if (bucket_selection === 'round-robin') {
    const vuIndex = __VU > 0 ? __VU - 1 : 0;
    container = container_list[vuIndex % container_list.length];
}
const grpc_clients = (grpc_endpoint ? [grpc_endpoint] : grpc_endpoints).map(e => native.connect(e, '', __ENV.DIAL_TIMEOUT ? parseInt(__ENV.DIAL_TIMEOUT) : 5, __ENV.STREAM_TIMEOUT ? parseInt(__ENV.STREAM_TIMEOUT) : 15));

function pick_client() {
    return grpc_clients[Math.floor(Math.random() * grpc_clients.length)];
}

if (__VU > 0) {
    console.log(`VU ID: ${__VU}, Bucket: ${container || 'random for each request'}, Endpoint: ${grpc_endpoint || 'random for each request'}`);
}

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
        throw new Error('Positive DELETE worker number without a proper object selector');
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

    console.log(`Pregenerated containers:       ${container_list.length}`);
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

function select_container() {
    return container || container_list[Math.floor(Math.random() * container_list.length)];
}

export function obj_write() {
    if (__ENV.SLEEP_WRITE) {
        sleep(__ENV.SLEEP_WRITE);
    }

    const headers = {
        unique_header: uuidv4()
    };
    const vu_container = select_container();

    const { payload, hash } = generator.genPayload(registry_enabled);
    const resp = pick_client().put(vu_container, headers, payload);
    if (!resp.success) {
        console.log({cid: vu_container, error: resp.error});
        return;
    }

    if (obj_registry) {
        obj_registry.addObject(vu_container, resp.object_id, "", "", hash);
    }
}

function select_object() {
    const vu_container = select_container();

    const selected_obj_list = obj_list_by_container[vu_container] || object_list;
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

    const resp = pick_client().get(obj.container, obj.object)
    if (!resp.success) {
        console.log({cid: obj.container, oid: obj.object, error: resp.error});
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

    const resp = pick_client().head(obj.container, obj.object)
    if (!resp.success) {
        console.log({cid: obj.container, oid: obj.object, error: resp.error});
    }
}

export function obj_list() {
    if (__ENV.SLEEP_LIST) {
        sleep(__ENV.SLEEP_LIST);
    }

    const vu_container = select_container();
    const resp = pick_client().list(vu_container, list_prefix);
    if (!resp.success) {
        console.log({cid: vu_container, error: resp.error});
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

    const resp = pick_client().delete(obj.c_id, obj.o_id);
    if (!resp.success) {
        // Log errors except (2052 - object already deleted)
        console.log({cid: obj.c_id, oid: obj.o_id, error: resp.error});
        return;
    }

    obj_registry.deleteObject(obj.c_id, "", obj.id);
}

export function uuidv4() {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
        let r = Math.random() * 16 | 0, v = c === 'x' ? r : (r & 0x3 | 0x8);
        return v.toString(16);
    });
}

export function handleSummary(data) {
    const neo_ver = (__ENV.NEO_VER || 'Neo_ver_not_set');
    const test_type = (__ENV.TEST_TYPE || 'Test_type_not_set');
    const run_id = (__ENV.RUN_ID || 'Run_id_not_set');
    const test_id = (__ENV.TEST_ID || 'Test_id_not_set');
    const size_id = (__ENV.SIZE_ID || 'Size_id_not_set');
    const vu_qty = (__ENV.VU_QTY || 'Vu_qty_not_set');
    const host_id = (__ENV.HOST_ID || 'Host_id_not_set');
    const result_format = (__ENV.RESULT_FORMAT || 'not_set');
    const c1 = ~~((data.metrics.data_received.values.rate || '0')/1024/1024);
    const c2 = ~~((data.metrics.data_sent.values.rate || '0')/1024/1024);
    const e3 = (data.metrics.neofs_obj_get_fails && data.metrics.neofs_obj_get_fails.values.rate || '0');
    const c3 = ~~((data.metrics.neofs_obj_get_total && data.metrics.neofs_obj_get_total.values.rate || '0') - e3);
    const c4 = ~~(data.metrics.neofs_obj_get_duration && data.metrics.neofs_obj_get_duration.values.avg || '0');
    const c5 = ~~(data.metrics.neofs_obj_get_duration && data.metrics.neofs_obj_get_duration.values["p(95)"] || '0');
    const e6 = (data.metrics.neofs_obj_put_fails && data.metrics.neofs_obj_put_fails.values.rate || '0');
    const c6 = ~~((data.metrics.neofs_obj_put_total && data.metrics.neofs_obj_put_total.values.rate || '0') - e6);
    const c7 = ~~(data.metrics.neofs_obj_put_duration && data.metrics.neofs_obj_put_duration.values.avg || '0');
    const c8 = ~~(data.metrics.neofs_obj_put_duration && data.metrics.neofs_obj_put_duration.values["p(95)"] || '0');
    const e9 = (data.metrics.neofs_obj_delete_fails && data.metrics.neofs_obj_delete_fails.values.rate || '0');
    const c9 = ~~((data.metrics.neofs_obj_delete_total && data.metrics.neofs_obj_delete_total.values.rate || '0') - e9);
    const c10 = ~~(data.metrics.neofs_obj_delete_duration && data.metrics.neofs_obj_delete_duration.values.avg || '0');
    const c11 = ~~(data.metrics.neofs_obj_delete_duration && data.metrics.neofs_obj_delete_duration.values["p(95)"] || '0');
    
    if (result_format == 'terse') {    
        return {
            stdout: `${neo_ver};${test_type};${run_id};${test_id};${size_id};${vu_qty};${host_id};${run_id}_${test_id}_${size_id}k_${vu_qty}vu_${neo_ver};${c1};${c2};${c3};${c4};${c5};${c6};${c7};${c8};${c9};${c10};${c11}\n`
        };
    }
}
