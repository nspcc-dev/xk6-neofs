import { check, fail } from 'k6';
import tree from 'k6/x/neofs/tree';
import { randomIntBetween } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';
import { htmlReport } from "https://raw.githubusercontent.com/benc-uk/k6-reporter/2.4.0/dist/bundle.js";

const grpc_endpoints = __ENV.GRPC_ENDPOINTS.split(',') || fail("provide GRPC_ENDPOINT when starting k6");
const client = tree.client(grpc_endpoints[randomIntBetween(0, grpc_endpoints.length - 1)]);
const sys_num = parseInt(__ENV.SYSTEM_WRITERS_NUM);
const user_num = parseInt(__ENV.USER_WRITERS_NUM);

export const options = {
    scenarios: {
        system_write: {
            executor: 'constant-arrival-rate',
            preAllocatedVUs: Math.ceil(sys_num / 10),
            maxVUs: sys_num*5, // 5s allowed deadline for 1s `timeUnit` requires x5 reserve
            rate: sys_num,
            timeUnit: "1s",
            duration: `${__ENV.DURATION}s`,
            exec: 'add',
            gracefulStop: '5s',
        },
        user_write: {
            executor: 'constant-arrival-rate',
            preAllocatedVUs: Math.ceil(user_num / 10),
            maxVUs: user_num*5, // 5s allowed deadline for 1s `timeUnit` requires x5 reserve
            rate: user_num,
            timeUnit: "1s",
            duration: `${__ENV.DURATION}s`,
            exec: 'add_by_path',
            gracefulStop: '5s',
        }
    },
    setupTimeout: '5s',
};

export function setup() {
    console.log(`System writing VUs: ${sys_num}`);
    console.log(`User writing VUs: ${user_num}`);
}

// add emulates system operations or not really often operations like
// initiating multipart uploads.
export function add() {
    const resp = client.add("system");
    check(resp, {
            'tree add': (r) => {
                if (!r.success) console.log(r.error);
                return r.success;
            }
        }
    )
}

// add_by_path emulates normal object (or part) puts.
export function add_by_path() {
    const resp = client.addByPath("version");
    check(resp, {
            'tree add_by_path': (r) => {
                if (!r.success) console.log(r.error);
                return r.success;
            }
        }
    )
}

export function handleSummary(data) {
    return {
        "summary.html": htmlReport(data),
    };
}
