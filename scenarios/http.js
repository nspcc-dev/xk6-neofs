import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';
import http from 'k6/http';
import crypto from 'k6/crypto';

/* 
   Parse profile from env.
   Format write:obj_size:
     * write    - write operations in percent, relative to read operations
     * obj_size - size of objects in kilobytes
     * duration - duration in seconds
*/
let [ write, obj_size, duration ] = __ENV.PROFILE.split(':');
// Set VUs between write and read operations.
let vus_read = Math.ceil(__ENV.CLIENTS/100*(100-parseInt(write)))
let vus_write = __ENV.CLIENTS - vus_read

const payload = crypto.randomBytes(1024*parseInt(obj_size))


let scenarios = {}

if (vus_write > 0){
    scenarios.write= {
        executor: 'constant-vus',
        vus: vus_write,
        duration: `${duration}s`,
        exec: 'obj_write', // the function this scenario will execute
        gracefulStop: '5s',
    }
}

if (vus_read > 0){
    scenarios.read= {
        executor: 'constant-vus',
        vus: vus_read,
        duration: `${duration}s`,
        exec: 'obj_read', 
        gracefulStop: '5s',
    }
}

export const options = {
    scenarios: scenarios
};

export function setup() {
    let obj_list = []

    // Prepare objects
    for (let i = 0; i < __ENV.PRELOAD_OBJ; i++) { 

        let data = {
            field: uuidv4(),
            file: http.file(payload, "random.data"),
        };
        let resp = http.post(`http://node1.neofs/upload/${__ENV.CID}`, data);

        if (resp.status === 200) {
            obj_list.push(resp.body.split('"')[3])
        }       
    }
    return { obj_list: obj_list };
  }

export function obj_write() {
    let data = {
        field: uuidv4(),
        file: http.file(payload, "random.data"),
    };
    let resp = http.post(`http://node1.neofs/upload/${__ENV.CID}`, data);
    if (resp.status != 200) {
        console.log(`${oid} - ${resp.status}`);
    }
}

export function obj_read(data) {
   let oid = data.obj_list[Math.floor(Math.random()*data.obj_list.length)];
   let resp = http.get(`http://node1.neofs/get/${__ENV.CID}/${oid}`);
   
   if (resp.status != 200) {
       console.log(`${oid} - ${resp.status}`);
   }
}
