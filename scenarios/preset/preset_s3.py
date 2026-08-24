#!/usr/bin/python3

import argparse
import json
import time
from concurrent.futures import ThreadPoolExecutor, as_completed

from helpers.aws_s3 import create_bucket, init_clients, upload_object
from helpers.cmd import endpoint_for, parse_endpoints, random_payload

parser = argparse.ArgumentParser()

parser.add_argument('--size', help='Upload objects size in kb.')
parser.add_argument('--buckets', help='Number of buckets to create.')
parser.add_argument('--out', help='JSON file with output.')
parser.add_argument('--preload_obj', help='Number of pre-loaded objects.')
parser.add_argument('--endpoint', help='S3 Gateway address. Comma-separated list spreads puts across gateways.')
parser.add_argument('--update', help='True/False, False by default. Save existed buckets from target file (--out). '
                                     'New buckets will not be created.')
parser.add_argument('--location', help='AWS location. Will be empty, if has not be declared.', default="")
parser.add_argument('--versioning', help='True/False, False by default.')
parser.add_argument('--workers', type=int, help='Number of workers (default 50)', default=50)

args = parser.parse_args()
print(args)


def main():
    bucket_list = []
    objects_struct = []
    payload_filepath = '/tmp/data_file_' + args.size + 'k'
    workers = args.workers
    preload_obj = int(args.preload_obj)
    endpoints = parse_endpoints(args.endpoint)

    print(f" > Endpoints: {endpoints}")
    init_clients(endpoints, workers)

    if args.update:
        with open(args.out) as f:
            data_json = json.load(f)
            bucket_list = data_json['buckets']
    else:
        print(f"Create buckets: {args.buckets}")
        with ThreadPoolExecutor(max_workers=workers) as executor:
            futures = [
                executor.submit(
                    create_bucket,
                    endpoint_for(endpoints, i),
                    args.versioning,
                    args.location,
                )
                for i in range(int(args.buckets))
            ]
            for run in as_completed(futures):
                bucket = run.result()
                if bucket:
                    bucket_list.append(bucket)
        print("Create buckets: Completed")

    print(f" > Buckets: {bucket_list}")

    print(f"Upload objects to each bucket: {args.preload_obj} ")
    random_payload(payload_filepath, args.size)
    print(" > Create random payload: Completed")

    start = time.monotonic()
    with ThreadPoolExecutor(max_workers=workers) as executor:
        futures = {
            executor.submit(upload_object, bucket, payload_filepath, endpoints, i): bucket
            for i, bucket in enumerate(
                bucket for bucket in bucket_list for _ in range(preload_obj)
            )
        }
        for run in as_completed(futures):
            object_name = run.result()
            if object_name:
                objects_struct.append({'bucket': futures[run], 'object': object_name})

    elapsed = time.monotonic() - start
    rate = len(objects_struct) / elapsed if elapsed else 0
    print(f"Upload objects to each bucket: Completed "
          f"({len(objects_struct)} objects in {elapsed:.1f}s, {rate:.0f} obj/s)")

    data = {'buckets': bucket_list, 'objects': objects_struct, 'obj_size': args.size + " Kb"}

    with open(args.out, 'w+') as f:
        json.dump(data, f, ensure_ascii=False, indent=2)

    print("Result:")
    print(f" > Total Buckets has been created: {len(bucket_list)}.")
    print(f" > Total Objects has been created: {len(objects_struct)}.")


if __name__ == "__main__":
    main()
