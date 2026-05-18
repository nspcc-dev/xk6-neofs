import native from 'k6/x/neofs/native';
import registry from 'k6/x/neofs/registry';
import s3 from 'k6/x/neofs/s3';
import http from 'k6/http';
import crypto from 'k6/crypto';
import { sleep } from 'k6';
import { Counter } from 'k6/metrics';

const obj_registry = registry.open(__ENV.REGISTRY_FILE);

// Time limit (in seconds) for the run
const time_limit = __ENV.TIME_LIMIT || "60";

// Number of objects in each status. These counters are cumulative in a
// sense that they reflect total number of objects in the registry, not just
// number of objects that were processed by specific run of this scenario.
// This allows to run this scenario multiple times and collect overall
// statistics in the final run.
const obj_counters = {
    verified: new Counter('verified_obj'),
    skipped: new Counter('skipped_obj'),
    invalid: new Counter('invalid_obj'),
};

// Connect to random gRPC endpoint
let grpc_client = undefined;
if (__ENV.GRPC_ENDPOINTS) {
    const grpcEndpoints = __ENV.GRPC_ENDPOINTS.split(',');
    const grpcEndpoint = grpcEndpoints[Math.floor(Math.random() * grpcEndpoints.length)];
    grpc_client = native.connect(grpcEndpoint, '', __ENV.DIAL_TIMEOUT ? parseInt(__ENV.DIAL_TIMEOUT) : 0, __ENV.STREAM_TIMEOUT ? parseInt(__ENV.STREAM_TIMEOUT) : 0);
}

// Connect to random S3 endpoint
let s3_client = undefined;
if (__ENV.S3_ENDPOINTS) {
    const s3_endpoints = __ENV.S3_ENDPOINTS.split(',');
    const s3_endpoint = s3_endpoints[Math.floor(Math.random() * s3_endpoints.length)];
    s3_client = s3.connect(`http://${s3_endpoint}`);
}

// Pick a random Morph endpoint. Morph objects are stored in the registry using
// the s3_bucket/s3_key slots, so when MORPH_ENDPOINTS is set we route those
// rows through this client instead of the s3 one.
let morph_endpoint = undefined;
let morph_auth_header = undefined;
if (__ENV.MORPH_ENDPOINTS) {
    const morph_endpoints = __ENV.MORPH_ENDPOINTS.split(',');
    morph_endpoint = morph_endpoints[Math.floor(Math.random() * morph_endpoints.length)].replace(/\/+$/, '');
    if (!__ENV.MORPH_AUTH_TOKEN) {
        throw new Error('MORPH_AUTH_TOKEN env var is required when MORPH_ENDPOINTS is set');
    }
    morph_auth_header = `Bearer ${__ENV.MORPH_AUTH_TOKEN}`;
}

// We will attempt to verify every object in "created" status. The scenario will execute
// as many iterations as there are objects. Each object will have 3 retries to be verified
const obj_to_verify_selector = registry.getSelector(
    __ENV.REGISTRY_FILE,
    "obj_to_verify",
    __ENV.SELECTION_SIZE ? parseInt(__ENV.SELECTION_SIZE) : 0,
    {
        status: "created",
    }
);
const obj_to_verify_count = obj_to_verify_selector.count();
// Execute at least one iteration (executor shared-iterations can't run 0 iterations)
const iterations = Math.max(1, obj_to_verify_count);
// Executor shared-iterations requires number of iterations to be larger than number of VUs
const vus = Math.min(__ENV.CLIENTS, iterations);

const scenarios = {
    verify: {
        executor: 'shared-iterations',
        vus,
        iterations,
        maxDuration: `${time_limit}s`,
        exec: 'obj_verify',
        gracefulStop: '5s',
    }
};

export const options = {
    scenarios,
    setupTimeout: '5s',
};

export function setup() {
    // Populate counters with initial values
    for (const [status, counter] of Object.entries(obj_counters)) {
        const obj_selector = registry.getSelector(
            __ENV.REGISTRY_FILE,
            status,
            __ENV.SELECTION_SIZE ? parseInt(__ENV.SELECTION_SIZE) : 0,
            { status });
        counter.add(obj_selector.count());
    }
}

export function obj_verify() {
    if (__ENV.SLEEP) {
        sleep(__ENV.SLEEP);
    }

    const obj = obj_to_verify_selector.nextObject();
    if (!obj) {
        console.log("All objects have been verified");
        return;
    }

    const obj_status = verify_object_with_retries(obj, 3);
    obj_counters[obj_status].add(1);
    obj_registry.setObjectStatus(obj.id, obj_status);
}

function verify_object_with_retries(obj, attempts) {
    for (let i = 0; i < attempts; i++) {
        let result;
        if (obj.c_id && obj.o_id) {
            result = grpc_client.verifyHash(obj.c_id, obj.o_id, obj.payload_hash);
        } else if (obj.s3_bucket && obj.s3_key) {
            if (morph_endpoint) {
                result = morph_verify_hash(obj.s3_bucket, obj.s3_key, obj.payload_hash);
            } else {
                result = s3_client.verifyHash(obj.s3_bucket, obj.s3_key, obj.payload_hash);
            }
        } else {
            console.log(`Object id=${obj.id} cannot be verified with supported protocols`);
            return "skipped";
        }

        if (result.success) {
            return "verified";
        } else if (result.error == "hash mismatch") {
            return "invalid";
        }

        // Unless we explicitly saw that there was a hash mismatch, then we will retry after a delay
        console.log(`Verify error on ${obj.id}: ${result.error}. Object will be re-tried`);
        sleep(__ENV.SLEEP);
    }

    return "invalid";
}

// morph_verify_hash mirrors the (gRPC|S3) verifyHash helpers exposed from Go:
// it GETs the object body, computes its sha256, and compares against the
// expected hash recorded at PUT time.
function morph_verify_hash(bucket, key, expected_hash) {
    const url = `${morph_endpoint}/api/v1/buckets/${bucket}/objects/${encodeURIComponent(key)}`;
    const resp = http.get(url, {
        headers: { Authorization: morph_auth_header },
        responseType: 'binary',
        tags: { op: 'morph_verify_get' },
    });
    if (resp.status !== 200) {
        return { success: false, error: `status ${resp.status}` };
    }
    const actual_hash = crypto.sha256(resp.body, 'hex');
    if (actual_hash !== expected_hash) {
        // Match the Go verifyHash convention: `success: true` + an error string
        // distinguishes "hash mismatch" from transport-level errors so the
        // retry loop above knows not to retry.
        return { success: true, error: 'hash mismatch' };
    }
    return { success: true };
}
