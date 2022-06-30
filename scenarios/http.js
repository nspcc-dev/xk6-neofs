import http from 'k6/http';
import crypto from 'k6/crypto';
import { SharedArray } from 'k6/data';
import { sleep } from 'k6';

const obj_list = new SharedArray('obj_list', function () {
    return JSON.parse(open(__ENV.PREGEN_JSON)).objects; });

const container_list = new SharedArray('container_list', function () {
    return JSON.parse(open(__ENV.PREGEN_JSON)).containers; });

const read_size = JSON.parse(open(__ENV.PREGEN_JSON)).obj_size;

/* 
   Parse profile from env.
   Format write:obj_size:
     * write    - write operations in percent, relative to read operations
     * duration - duration in seconds
*/

const [ write, duration ] = __ENV.PROFILE.split(':');

// Set VUs between write and read operations
let vus_read = Math.ceil(__ENV.CLIENTS/100*(100-parseInt(write)))
let vus_write = __ENV.CLIENTS - vus_read

const payload = crypto.randomBytes(1024*parseInt(__ENV.WRITE_OBJ_SIZE))

let nodes = __ENV.NODES.split(',') // node1.neofs
let rand_node = nodes[Math.floor(Math.random()*nodes.length)];

let scenarios = {}

if (vus_write > 0){
    scenarios.write= {
        executor: 'constant-vus',
        vus: vus_write,
        duration: `${duration}s`,
        exec: 'obj_write', 
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

export function setup() {
    console.log("Pregenerated containers: " + container_list.length)
    console.log("Pregenerated read object size: " + read_size)
    console.log("Pregenerated total objects: " + obj_list.length)
}

export const options = {
    scenarios: scenarios,
    setupTimeout: '5s',
};

export function obj_write() {
    let data = {
        field: uuidv4(),
        file: http.file(payload, "random.data"),
    };
    let container = container_list[Math.floor(Math.random()*container_list.length)];

    let resp = http.post(`http://${rand_node}/upload/${container}`, data);
    if (resp.status != 200) {
        console.log(`${resp.status}`);
    }
    //sleep(1)
}

export function obj_read() {
    let random_read_obj = obj_list[Math.floor(Math.random()*obj_list.length)];
    let resp = http.get(`http://${rand_node}/get/${random_read_obj.container}/${random_read_obj.object}`);
    if (resp.status != 200) {
        console.log(`${random_read_obj.object} - ${resp.status}`);
    }
}

export function uuidv4() {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
      let r = Math.random() * 16 | 0, v = c === 'x' ? r : (r & 0x3 | 0x8);
      return v.toString(16);
    });
  }
