#!/usr/bin/python3

"""Pre-generate Morph REST API buckets/objects for the `scenarios/morph.js`
load test. Output JSON shape matches `preset_s3.py` so the same
`PREGEN_JSON` env var can be passed to the k6 scenario.

Requires the `requests` package.
"""

import argparse
import base64
import json
import os
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed

import requests
from urllib.parse import quote

from helpers.cmd import random_payload

parser = argparse.ArgumentParser()

parser.add_argument('--size', help='Upload objects size in kb.')
parser.add_argument('--buckets', help='Number of buckets to create.')
parser.add_argument('--out', help='JSON file with output.')
parser.add_argument('--preload_obj', help='Number of pre-loaded objects per bucket.')
parser.add_argument('--endpoint', help='Morph REST API base URL, e.g. http://host:80')
parser.add_argument('--auth_token', help='Bearer token for the Morph REST API.')
parser.add_argument('--policy', help='Placement policy name (passed as placementPolicyName).',
                    default='default')
parser.add_argument('--update', help='True/False, False by default. Reuse existing buckets '
                                     'from target file (--out) instead of creating new ones.')
parser.add_argument('--workers', type=int, help='Number of workers (default 50)', default=50)

args = parser.parse_args()
print(args)


def _headers(extra=None):
    headers = {
        'Authorization': f'Bearer {args.auth_token}',
        'Content-Type': 'application/json',
    }
    if extra:
        headers.update(extra)
    return headers


def _base_url():
    return args.endpoint.rstrip('/')


def create_bucket(_):
    bucket_name = str(uuid.uuid4())
    payload = {
        'name': bucket_name,
        'placementPolicyName': args.policy,
        'basicACL': {
            'final': False,
            'sticky': False,
            'owner': {
                'readContent': True, 'readHeaders': True, 'create': True, 'delete': True,
            },
            'others': {
                'readContent': True, 'readHeaders': True, 'create': False, 'delete': False,
            },
        },
    }
    try:
        resp = requests.post(
            f'{_base_url()}/api/v1/buckets',
            headers=_headers(), data=json.dumps(payload), timeout=30,
        )
    except requests.RequestException as exc:
        print(f' > Bucket {bucket_name} request failed: {exc}')
        return None
    if not resp.ok:
        print(f' > Bucket {bucket_name} not created: {resp.status_code} {resp.text}')
        return None
    return bucket_name


def upload_object(bucket, payload_filepath):
    MAX_RETRIES = 5
    object_key = str(uuid.uuid4())
    encoded_path = base64.b64encode(object_key.encode()).decode()

    with open(payload_filepath, 'rb') as fp:
        data = fp.read()

    headers = _headers({
        'Content-Type': 'application/octet-stream',
        'x-morph-path': encoded_path,
    })

    for attempt in range(MAX_RETRIES):
        try:
            resp = requests.post(
                f'{_base_url()}/api/v1/buckets/{bucket}/objects',
                headers=headers, data=data, timeout=60,
            )
        except requests.RequestException as exc:
            print(f' > Object {object_key} attempt {attempt + 1} failed: {exc}')
            continue
        if resp.ok:
            return object_key
        print(f' > Object {object_key} attempt {attempt + 1} not uploaded: '
              f'{resp.status_code} {resp.text}')

    print(f' > Object {object_key} not uploaded after {MAX_RETRIES} attempts.')
    return None


def main():
    if not args.auth_token:
        raise SystemExit('--auth_token is required')

    bucket_list = []
    objects_struct = []
    payload_filepath = '/tmp/data_file'

    if args.update:
        with open(args.out) as f:
            data_json = json.load(f)
            bucket_list = data_json['buckets']
    else:
        print(f'Create buckets: {args.buckets}')

        with ThreadPoolExecutor(max_workers=args.workers) as executor:
            futures = [executor.submit(create_bucket, idx) for idx in range(int(args.buckets))]

        for run in as_completed(futures):
            result = run.result()
            if result is not None:
                bucket_list.append(result)

        print('Create buckets: Completed')

    print(f' > Buckets: {bucket_list}')

    print(f'Upload objects to each bucket: {args.preload_obj}')
    random_payload(payload_filepath, args.size)
    print(' > Create random payload: Completed')

    for bucket in bucket_list:
        print(f' > Upload objects for bucket {bucket}')
        with ThreadPoolExecutor(max_workers=args.workers) as executor:
            futures = [
                executor.submit(upload_object, bucket, payload_filepath)
                for _ in range(int(args.preload_obj))
            ]
        for run in as_completed(futures):
            object_key = run.result()
            if object_key:
                objects_struct.append({'bucket': bucket, 'object': object_key})
        print(f' > Upload objects for bucket {bucket}: Completed')

    print('Upload objects to each bucket: Completed')

    data = {'buckets': bucket_list, 'objects': objects_struct, 'obj_size': args.size + ' Kb'}

    with open(args.out, 'w+') as f:
        json.dump(data, f, ensure_ascii=False, indent=2)

    print('Result:')
    print(f' > Total Buckets has been created: {len(bucket_list)}.')
    print(f' > Total Objects has been created: {len(objects_struct)}.')


if __name__ == '__main__':
    main()
