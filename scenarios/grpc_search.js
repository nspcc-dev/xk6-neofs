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
