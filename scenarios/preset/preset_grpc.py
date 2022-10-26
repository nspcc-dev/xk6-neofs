#!/usr/bin/python3

import argparse
import json
import os
import shlex
from concurrent.futures import ProcessPoolExecutor
from subprocess import check_output, CalledProcessError, STDOUT

parser = argparse.ArgumentParser()
parser.add_argument('--size', help='Upload objects size in kb')
parser.add_argument('--containers', help='Number of containers to create')
parser.add_argument('--out', help='JSON file with output')
parser.add_argument('--preload_obj', help='Number of pre-loaded objects')
parser.add_argument(
    "--policy",
    help="Container placement policy",
    default="REP 1 IN X CBF 1 SELECT 1 FROM * AS X"
)
parser.add_argument('--endpoint', help='Node address')
parser.add_argument('--update', help='Save existed containers')

args = parser.parse_args()
print(args)


def main():
    container_list = []
    objects_struct = []
    payload_filepath = '/tmp/data_file'

    if args.update:
        # Open file
        with open(args.out) as f:
            data_json = json.load(f)
            container_list = data_json['containers']
    else:
        print(f"Create containers: {args.containers}")
        with ProcessPoolExecutor(max_workers=10) as executor:
            containers_runs = {executor.submit(create_container): _ for _ in range(int(args.containers))}

        for run in containers_runs:
            if run.result() is not None:
                container_list.append(run.result())

        print("Create containers: Completed")

    print(f" > Containers: {container_list}")
    if not container_list:
        return

    print(f"Upload objects to each container: {args.preload_obj} ")
    random_payload(payload_filepath)
    print(" > Create random payload: Completed")

    for container in container_list:
        print(f" > Upload objects for container {container}")
        with ProcessPoolExecutor(max_workers=50) as executor:
            objects_runs = {executor.submit(upload_object, container, payload_filepath): _ for _ in
                            range(int(args.preload_obj))}

        for run in objects_runs:
            if run.result() is not None:
                objects_struct.append({'container': container, 'object': run.result()})
        print(f" > Upload objects for container {container}: Completed")

    print("Upload objects to each container: Completed")

    data = {'containers': container_list, 'objects': objects_struct, 'obj_size': args.size + " Kb"}

    with open(args.out, 'w') as f:
        json.dump(data, f, ensure_ascii=False)

    print(f"Result:")
    print(f" > Total Containers has been created: {len(container_list)}.")
    print(f" > Total Objects has been created: {len(objects_struct)}.")


def random_payload(payload_filepath):
    with open('%s' % payload_filepath, 'wb') as fout:
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


def create_container():
    cmd_line = f"neofs-cli --rpc-endpoint {args.endpoint} container create -g --policy '{args.policy}' --basic-acl public-read-write --await"
    output, success = execute_cmd(cmd_line)

    if not success:
        print(f" > Container has not been created.")
    else:
        try:
            fst_str = output.split('\n')[0]
        except Exception:
            print(f"Got empty output: {output}")
            return
        splitted = fst_str.split(": ")
        if len(splitted) != 2:
            raise ValueError(f"no CID was parsed from command output: \t{fst_str}")
        return splitted[1]


def upload_object(container, payload_filepath):
    object_name = ""
    cmd_line = f"neofs-cli --rpc-endpoint {args.endpoint} object put -g --file {payload_filepath} --cid {container} --no-progress"
    out, success = execute_cmd(cmd_line)

    if not success:
        print(f" > Object {object_name} has not been uploaded.")
    else:
        try:
            # taking second string from command output
            snd_str = out.split('\n')[1]
        except:
            print(f"Got empty input: {out}")
            return
        splitted = snd_str.split(": ")
        if len(splitted) != 2:
            raise Exception(f"no OID was parsed from command output: \t{snd_str}")
        return splitted[1]


if __name__ == "__main__":
    main()
