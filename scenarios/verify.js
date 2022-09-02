import native from 'k6/x/neofs/native';
import registry from 'k6/x/neofs/registry';
import s3 from 'k6/x/neofs/s3';
import { sleep } from 'k6';
import exec from 'k6/execution';

/*
   ./k6 run -e CLIENTS=200 -e TIME_LIMIT=30 -e GRPC_ENDPOINTS=node4.data:8084 scenarios/verify.js
*/

// Time limit (in seconds) for the run
const time_limit = __ENV.TIME_LIMIT || "60";

// Connect to random gRPC endpoint
let grpc_client = undefined;
if (__ENV.GRPC_ENDPOINTS) {
    const grpcEndpoints = __ENV.GRPC_ENDPOINTS.split(',');
    const grpcEndpoint = grpcEndpoints[Math.floor(Math.random() * grpcEndpoints.length)];
    grpc_client = native.connect(grpcEndpoint, '');
}

// Connect to random S3 endpoint
let s3_client = undefined;
if (__ENV.S3_ENDPOINTS) {
    const s3_endpoints = __ENV.S3_ENDPOINTS.split(',');
    const s3_endpoint = s3_endpoints[Math.floor(Math.random() * s3_endpoints.length)];
    s3_client = s3.connect(`http://${s3_endpoint}`);
}

const scenarios = {
    verify: {
        executor: 'constant-vus',
        vus: __ENV.CLIENTS,
        duration: `${time_limit}s`,
        exec: 'obj_verify',
        gracefulStop: '5s',
    }
};

export const options = {
    scenarios: scenarios,
    setupTimeout: '5s',
};

export function obj_verify() {
    if (__ENV.SLEEP) {
        sleep(__ENV.SLEEP);
    }

    const obj = registry.nextObjectToVerify();
    if (!obj) {
        // TODO: consider using a metric with abort condition to stop execution when
        // all VUs have no objects to verify. Alternative solution could be a
        // shared-iterations executor, but it might be not a good choice, as we need to
        // check same object several times (if specific request fails)

        // Allow time for other VUs to complete verification
        sleep(30.0);
        exec.test.abort("All objects have been verified");
    }
    console.log(`Verifying object ${obj.id}`);

    let result = undefined;
    if (obj.c_id && obj.o_id) {
        result = grpc_client.verifyHash(obj.c_id, obj.o_id, obj.payload_hash);
    } else if (obj.s3_bucket && obj.s3_key) {
        result = s3_client.verifyHash(obj.s3_bucket, obj.s3_key, obj.payload_hash);
    } else {
        console.log(`Object id=${obj.id} cannot be verified with supported protocols`);
        registry.setObjectStatus(obj.id, "skipped");
    }

    if (result.success) {
        registry.setObjectStatus(obj.id, "verified");
    } else {
        registry.setObjectStatus(obj.id, "invalid");
        console.log(`Verify error on ${obj.c_id}/${obj.o_id}: {resp.error}`);
    }
}
