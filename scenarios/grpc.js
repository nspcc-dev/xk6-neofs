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

// Select random gRPC endpoint for current VU
const grpc_endpoints = __ENV.GRPC_ENDPOINTS.split(',');
const grpc_endpoint = grpc_endpoints[Math.floor(Math.random() * grpc_endpoints.length)];
const grpc_client = native.connect(grpc_endpoint, '', __ENV.DIAL_TIMEOUT ? parseInt(__ENV.DIAL_TIMEOUT) : 5, __ENV.STREAM_TIMEOUT ? parseInt(__ENV.STREAM_TIMEOUT) : 15);

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
};

export function setup() {
    const total_vu_count = write_vu_count + read_vu_count + delete_vu_count;

    console.log(`Pregenerated containers:       ${container_list.length}`);
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

    const headers = {
        unique_header: uuidv4()
    };
    const container = container_list[Math.floor(Math.random() * container_list.length)];

    const { payload, hash } = generator.genPayload(registry_enabled);
    const resp = grpc_client.put(container, headers, payload);
    if (!resp.success) {
        console.log({cid: container, error: resp.error});
        return;
    }

    if (obj_registry) {
        obj_registry.addObject(container, resp.object_id, "", "", hash);
    }
}

export function obj_read() {
    if (__ENV.SLEEP_READ) {
        sleep(__ENV.SLEEP_READ);
    }

    const obj = obj_list[Math.floor(Math.random() * obj_list.length)];
    const resp = grpc_client.get(obj.container, obj.object)
    if (!resp.success) {
        console.log({cid: obj.container, oid: obj.object, error: resp.error});
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

    const resp = grpc_client.delete(obj.c_id, obj.o_id);
    if (!resp.success) {
        // Log errors except (2052 - object already deleted)
        console.log({cid: obj.c_id, oid: obj.o_id, error: resp.error});
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
    const c3 = ~~(data.metrics.neofs_obj_get_total && data.metrics.neofs_obj_get_total.values.rate || '0');
    const c4 = ~~(data.metrics.neofs_obj_get_duration && data.metrics.neofs_obj_get_duration.values.avg || '0');
    const c5 = ~~(data.metrics.neofs_obj_get_duration && data.metrics.neofs_obj_get_duration.values["p(95)"] || '0');
    const c6 = ~~(data.metrics.neofs_obj_put_total && data.metrics.neofs_obj_put_total.values.rate || '0');
    const c7 = ~~(data.metrics.neofs_obj_put_duration && data.metrics.neofs_obj_put_duration.values.avg || '0');
    const c8 = ~~(data.metrics.neofs_obj_put_duration && data.metrics.neofs_obj_put_duration.values["p(95)"] || '0');
    const c9 = ~~(data.metrics.neofs_obj_delete_total && data.metrics.neofs_obj_delete_total.values.rate || '0');
    const c10 = ~~(data.metrics.neofs_obj_delete_duration && data.metrics.neofs_obj_delete_duration.values.avg || '0');
    const c11 = ~~(data.metrics.neofs_obj_delete_duration && data.metrics.neofs_obj_delete_duration.values["p(95)"] || '0');
    
    if (result_format == 'terse') {    
        return {
            stdout: `${neo_ver};${test_type};${run_id};${test_id};${size_id};${vu_qty};${host_id};${run_id}_${test_id}_${size_id}k_${vu_qty}vu_${neo_ver};${c1};${c2};${c3};${c4};${c5};${c6};${c7};${c8};${c9};${c10};${c11}\n`
        };
    }
}
