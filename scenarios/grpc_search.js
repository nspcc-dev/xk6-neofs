import { check } from 'k6';
import native from 'k6/x/neofs/native';

const dial_timeout = 5
const stream_timeout = 15
const predefined_private_key = '' // usually no need for search requests in load tests
const grpc_client = native.connect(__ENV.GRPC_ENDPOINT, predefined_private_key, dial_timeout, stream_timeout);
const container = __ENV.cid

export const options = {
    scenarios: {
        system_write: {
            executor: 'shared-iterations',
            vus: __ENV.vu,
            iterations: __ENV.i,
            exec: 'search',
            maxDuration: (24*365*100).toString()+"h", // default is 10m and this load is designed to be controlled by iterations only
            gracefulStop: '30s',
        },
    },
};

export function search() {
    let res = grpc_client.search(container, [{
        key: "test",
        operation: "STRING_EQUAL",
        value: "test"
    }])
    check(res, {
            'search': (r) => {
                return r > 0;
            }
        }
    )
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
    const c12 = ~~(data.metrics.iteration_duration && data.metrics.iteration_duration.values.avg || '0');
    const c13 = ~~(data.metrics.iteration_duration && data.metrics.iteration_duration.values["p(95)"] || '0');
            
    if (result_format == 'terse') {    
        return {
            stdout: `${neo_ver};${test_type};${run_id};${test_id};${size_id};${vu_qty};${host_id};${run_id}_${test_id}_${size_id}k_${vu_qty}vu_${neo_ver};${c1};${c2};${c3};${c4};${c5};${c6};${c7};${c8};${c9};${c10};${c11};${c12};${c13}\n`
        };
    }
}
