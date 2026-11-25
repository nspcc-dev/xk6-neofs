import os
import shlex
import sys
import threading

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


def parse_endpoints(raw):
    endpoints = [part.strip() for part in raw.split(',') if part.strip()]
    if not endpoints:
        raise ValueError('no endpoints provided')
    return endpoints


def endpoint_for(endpoints, index):
    return endpoints[index % len(endpoints)]


# Matches internal/datagen.TailSize; buffer refills after this many slices.
TAIL_SIZE = 1024


class PayloadGenerator:
    def __init__(self, size_kb, tail_size=TAIL_SIZE):
        size_kb = int(size_kb)
        if size_kb < 0:
            raise ValueError(f'size_kb must be non-negative, got {size_kb}')
        tail_size = int(tail_size)
        if tail_size <= 0:
            raise ValueError(f'tail_size must be positive, got {tail_size}')
        self._size_bytes = 1024 * size_kb
        self._tail_size = tail_size
        self._buf = None
        self._offset = 0
        self._lock = threading.Lock()

    def next_payload(self):
        if self._size_bytes == 0:
            return b''
        with self._lock:
            if self._buf is None:
                self._buf = os.urandom(self._size_bytes + self._tail_size)
                self._offset = 0
            buf = self._buf
            offset = self._offset
            self._offset += 1
            if self._offset >= self._tail_size:
                self._buf = None
                self._offset = 0
        return buf[offset:offset + self._size_bytes]


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
