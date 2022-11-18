import os
import shlex
import sys

from subprocess import check_output, CalledProcessError, STDOUT


def execute_cmd(cmd_line):
    cmd_args = shlex.split(cmd_line)

    try:
        output = check_output(cmd_args, stderr=STDOUT).decode()
        success = True

    except CalledProcessError as e:
        output = e.output.decode()
        success = False

    return output, success


def random_payload(payload_filepath, size):
    with open('%s' % payload_filepath, 'w+b') as fout:
        fout.write(os.urandom(1024 * int(size)))


class ProgressBar:
    @staticmethod
    def start():
        sys.stdout.write('\r\n\r\n')

    @staticmethod
    def print(current, goal):
        finish_percent = current / goal
        sys.stdout.write('\r')
        sys.stdout.write(f" > Progress: [{'=' * int(30 * finish_percent)}{' ' * (29 - int(30 * finish_percent))}>]"
                         f" {current}/{goal}")
        sys.stdout.flush()

    @staticmethod
    def end():
        sys.stdout.write('\r\n\r\n')
