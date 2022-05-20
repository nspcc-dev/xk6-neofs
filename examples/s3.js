import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';
import s3 from 'k6/x/neofs/s3';

const payload = open('../go.sum', 'b');
const bucket = "cats"
const s3_cli = s3.connect("http://s3.neofs.devenv:8080")

export const options = {
    stages: [
        { duration: '30s', target: 10 },
    ],
};

export default function () {
    const key = uuidv4();
    if (s3_cli.put(bucket, key, payload).success) {
        s3_cli.get(bucket, key )
    }
}
