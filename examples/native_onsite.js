import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';
import native from 'k6/x/neofs/native';

const payload = open('../go.sum', 'b');
const container = "AjSxSNNXbJUDPqqKYm1VbFVDGCakbpUNH8aGjPmGAH3B"
const neofs_cli = native.connect("s01.neofs.devenv:8080", "", 0, 0)
const neofs_obj = neofs_cli.onsite(container, payload)

export const options = {
    stages: [
        { duration: '30s', target: 10 },
    ],
};

export default function () {
    let headers = {
       'unique_header': uuidv4()
    }
    let resp = neofs_obj.put(headers)
    if (resp.success) {
       neofs_cli.get(container, resp.object_id)
    } else {
        console.log(resp.error)
    }
}
