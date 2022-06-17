import {uuidv4} from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';
import {fail} from "k6";
import native from 'k6/x/neofs/native';

const payload = open('../go.sum', 'b');
const neofs_cli = native.connect("s01.neofs.devenv:8080", "1dd37fba80fec4e6a6f13fd708d8dcb3b29def768017052f6c930fa1c5d90bbb")

export const options = {
    stages: [
        {duration: '30s', target: 10},
    ],
};

export function setup() {
    const params = {
        acl: 'public-read-write',
        placement_policy: 'REP 3',
        name: 'container-name',
        name_global_scope: 'false'
    }

    const res = neofs_cli.putContainer(params)
    if (!res.success) {
        fail(res.error)
    }
    console.info("created container", res.container_id)
    return {container_id: res.container_id}
}

export default function (data) {
    let headers = {
        'unique_header': uuidv4()
    }
    let resp = neofs_cli.put(data.container_id, headers, payload)
    if (resp.success) {
        neofs_cli.get(data.container_id, resp.object_id)
    } else {
        console.log(resp.error)
    }
}
