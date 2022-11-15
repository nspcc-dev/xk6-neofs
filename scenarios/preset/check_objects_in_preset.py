#!/usr/bin/python3

import argparse
import json
import shlex
from subprocess import check_output, CalledProcessError, STDOUT

parser = argparse.ArgumentParser()
parser.add_argument('--endpoint', help='Node address')
parser.add_argument('--preset_json', help='JSON file path with preset')
parser.add_argument('--print_success', help='Print objects that was successfully read', default=False)

args = parser.parse_args()


def main():
    with open(args.preset_json) as f:
        preset_text = f.read()

    preset = json.loads(preset_text)

    success_objs = 0
    failed_objs = 0

    for obj in preset.get('objects'):
        oid = obj.get('object')
        cid = obj.get('container')

        cmd_line = f"neofs-cli object get -r {args.endpoint} -g" \
                   f" --cid {cid} --oid {oid} --file /dev/null"

        output, success = execute_cmd(cmd_line)

        if success:
            success_objs += 1
            if args.print_success:
                print(f'Object: {oid} from {cid}: {"Ok" if success else "False"}')
        else:
            failed_objs += 1
            print(f'Object: {oid} from {cid}: {"Ok" if success else "False"}')

    print(f'Success objects: {success_objs}')
    print(f'Failed objects: {failed_objs}')


def execute_cmd(cmd_line):
    cmd_args = shlex.split(cmd_line)

    try:
        output = check_output(cmd_args, stderr=STDOUT).decode()
        success = True

    except CalledProcessError as e:
        output = e.output.decode()
        success = False

    return output, success


if __name__ == "__main__":
    main()