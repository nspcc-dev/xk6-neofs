import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';
import s3 from 'k6/x/neofs/s3';
import crypto from 'k6/crypto';

/* 
   Parse profile from env.
   Format write:obj_size:
     * write    - write operations in percent, relative to read operations
     * obj_size - size of objects in kilobytes
     * duration - duration in seconds
*/
let [ write, obj_size, duration ] = __ENV.PROFILE.split(':');
// Set VUs between write and read operations
let vus_read = Math.ceil(__ENV.CLIENTS/100*(100-parseInt(write)))
let vus_write = __ENV.CLIENTS - vus_read

const payload = crypto.randomBytes(1024*parseInt(obj_size))
const s3_cli = s3.connect("http://node1.intra:8084")

let scenarios = {}

if (vus_write > 0){
    scenarios.write= {
        executor: 'constant-vus',
        vus: vus_write,
        duration: `${duration}s`,
        exec: 'obj_write', // the function this scenario will execute
    }
}

if (vus_read > 0){
    scenarios.read= {
        executor: 'constant-vus',
        vus: vus_read,
        duration: `${duration}s`,
        exec: 'obj_read', 
    }
}

export const options = {
    scenarios: scenarios
};

export function setup() {
    let obj_list = []

    // Prepare objects
    for (let i = 0; i < __ENV.PRELOAD_OBJ; i++) { 
        let key = uuidv4();
        let resp = s3_cli.put(__ENV.BUCKET, key, payload)
        if (resp.success) {
            obj_list.push(key)
        }
    }
    return { obj_list: obj_list };
  }

export function obj_write() {
    let key = uuidv4();
    let resp = s3_cli.put(__ENV.BUCKET, key, payload)
    if (!resp.success) {
        console.log(resp.error);
    }
}

export function obj_read(data) {
   let key = data.obj_list[Math.floor(Math.random()*data.obj_list.length)];
   let resp = s3_cli.get(__ENV.BUCKET, key )
   if (!resp.success) {
       console.log(resp.error);
   }
}
