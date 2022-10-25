#!/usr/bin/python3

import argparse
import json
import os
import shlex
import uuid
from concurrent.futures import ProcessPoolExecutor
from subprocess import check_output, CalledProcessError, STDOUT

parser = argparse.ArgumentParser()

parser.add_argument('--size', help='Upload objects size in kb.')
parser.add_argument('--buckets', help='Number of buckets to create.')
parser.add_argument('--out', help='JSON file with output.')
parser.add_argument('--preload_obj', help='Number of pre-loaded objects.')
parser.add_argument('--endpoint', help='S3 Gateway address.')
parser.add_argument('--update', help='True/False, False by default. Save existed buckets from target file (--out). New buckets will not be created.')
parser.add_argument('--location', help='AWS location. Will be empty, if has not be declared.')
parser.add_argument('--versioning', help='True/False, False by default.')

args = parser.parse_args()
print(args)


def main():
    bucket_list = []
    objects_struct = []
    payload_filepath = '/tmp/data_file'

    if args.update:
        # Open file
        with open(args.out) as f:
            data_json = json.load(f)
            bucket_list = data_json['buckets']
        # Get CID list
    else:
        print(f"Create buckets: {args.buckets}")

        with ProcessPoolExecutor(max_workers=10) as executor:
            buckets_runs = {executor.submit(create_bucket): _ for _ in range(int(args.buckets))}

        for run in buckets_runs:
            if run.result() is not None:
                bucket_list.append(run.result())

        print("Create buckets: Completed")

    print(f" > Buckets: {bucket_list}")

    print(f"Upload objects to each bucket: {args.preload_obj} ")
    random_payload(payload_filepath)
    print(" > Create random payload: Completed")

    for bucket in bucket_list:
        print(f" > Upload objects for bucket {bucket}")
        with ProcessPoolExecutor(max_workers=50) as executor:
            objects_runs = {executor.submit(upload_object, bucket, payload_filepath): _ for _ in range(int(args.preload_obj))}

        for run in objects_runs:
            if run.result() is not None:
                objects_struct.append({'bucket': bucket, 'object': run.result()})
        print(f" > Upload objects for bucket {bucket}: Completed")

    print("Upload objects to each bucket: Completed")

    data = {'buckets': bucket_list, 'objects': objects_struct, 'obj_size': args.size + " Kb"}

    with open(args.out, 'w+') as f:
        json.dump(data, f, ensure_ascii=False)

    print(f"Result:")
    print(f" > Total Buckets has been created: {len(bucket_list)}.")
    print(f" > Total Objects has been created: {len(objects_struct)}.")


def random_payload(payload_filepath):
    with open('%s' % payload_filepath, 'w+b') as fout:
        fout.write(os.urandom(1024 * int(args.size)))


def execute_cmd(cmd_line):
    args = shlex.split(cmd_line)
    output = ""
    try:
        output = check_output(args, stderr=STDOUT).decode()
        success = True

    except CalledProcessError as e:
        output = e.output.decode()
        success = False

    return output, success


def create_bucket():
    bucket_create_marker = False

    location = ""
    if args.location:
        location = f"--create-bucket-configuration 'LocationConstraint={args.location}'"
    bucket_name = str(uuid.uuid4())

    cmd_line = f"aws --no-verify-ssl s3api create-bucket --bucket {bucket_name} --endpoint http://{args.endpoint} {location}"
    cmd_line_ver = f"aws --no-verify-ssl s3api put-bucket-versioning --bucket {bucket_name} --versioning-configuration Status=Enabled --endpoint http://{args.endpoint} "

    out, success = execute_cmd(cmd_line)

    if not success:
        if "succeeded and you already own it" in out:
            bucket_create_marker = True
        else:
            print(f" > Bucket {bucket_name} has not been created.")
    else:
        bucket_create_marker = True
        print(f"cmd: {cmd_line}")

    if bucket_create_marker and args.versioning == "True":
        out, success = execute_cmd(cmd_line_ver)
        if not success:
            print(f" > Bucket versioning has not been applied for bucket {bucket_name}.")
        else:
            print(f" > Bucket versioning has been applied.")

    return bucket_name


def upload_object(bucket, payload_filepath):
    object_name = str(uuid.uuid4())

    cmd_line = f"aws s3api put-object --bucket {bucket} --key {object_name} --body {payload_filepath} --endpoint http://{args.endpoint}"
    out, success = execute_cmd(cmd_line)

    if not success:
        print(f" > Object {object_name} has not been uploaded.")
    else:
        return object_name


if __name__ == "__main__":
    main()
