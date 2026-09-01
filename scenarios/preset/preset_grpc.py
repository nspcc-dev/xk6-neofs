#!/usr/bin/env python3

import argparse
import json
import multiprocessing
import os
import time
from concurrent.futures import ProcessPoolExecutor, ThreadPoolExecutor, as_completed
from os.path import expanduser

from helpers.cmd import endpoint_for, parse_endpoints, random_payload
from helpers.neofs_cli import create_container
from helpers.neofs_grpc import init_worker, upload_objects, worker_ready

parser = argparse.ArgumentParser()
parser.add_argument('--size', help='Upload objects size in kb')
parser.add_argument('--containers', help='Number of containers to create')
parser.add_argument('--out', help='JSON file with output')
parser.add_argument('--preload_obj', help='Number of pre-loaded objects')
parser.add_argument('--wallet', help='Wallet file path')
parser.add_argument('--config', help='Wallet config file path')
parser.add_argument(
    "--policy",
    help="Container placement policy",
    default="REP 2 IN X CBF 2 SELECT 2 FROM * AS X"
)
parser.add_argument('--endpoint', help='Node address. Comma-separated list spreads puts across storage nodes.')
parser.add_argument('--update', help='Save existed containers')
parser.add_argument('--workers', type=int, help='Number of concurrent uploads (default 50)', default=50)


def _job_chunks(jobs, nprocs):
    nprocs = max(1, min(nprocs, len(jobs)))
    return [jobs[i::nprocs] for i in range(nprocs)]


def main():
    args = parser.parse_args()
    print(args)

    container_list = []
    objects_struct = []
    payload_filepath = expanduser("~") + '/data_file_' + args.size + 'k'
    workers = args.workers
    preload_obj = int(args.preload_obj)

    endpoints = parse_endpoints(args.endpoint)
    print(f" > Endpoints: {endpoints}")

    wallet = args.wallet
    wallet_config = args.config
    nprocs = min(workers, os.cpu_count() or 1)
    threads = max(1, (workers + nprocs - 1) // nprocs)

    if args.update:
        with open(args.out) as f:
            data_json = json.load(f)
            container_list = data_json['containers']

    random_payload(payload_filepath, args.size)
    print(" > Create random payload: Completed")

    with ProcessPoolExecutor(
        max_workers=nprocs,
        mp_context=multiprocessing.get_context("spawn"),
        initializer=init_worker,
        initargs=(payload_filepath, endpoints, wallet, wallet_config),
    ) as executor:
        ready = [executor.submit(worker_ready) for _ in range(nprocs)]

        if not args.update:
            print(f"Create containers: {args.containers}")
            with ThreadPoolExecutor(max_workers=workers) as pool:
                futures = [
                    pool.submit(
                        create_container,
                        endpoint_for(endpoints, i),
                        args.policy,
                        wallet,
                        wallet_config,
                    )
                    for i in range(int(args.containers))
                ]
                for run in as_completed(futures):
                    container = run.result()
                    if container:
                        container_list.append(container)
            print("Create containers: Completed")

        print(f" > Containers: {container_list}")
        if not container_list:
            return

        for fut in ready:
            fut.result()

        print(f"Upload objects to each container: {args.preload_obj} ")
        jobs = [
            (container, i)
            for i, container in enumerate(
                container for container in container_list for _ in range(preload_obj)
            )
        ]
        chunks = _job_chunks(jobs, nprocs)

        start = time.monotonic()
        futures = [
            executor.submit(
                upload_objects,
                chunk,
                payload_filepath,
                endpoints,
                wallet,
                wallet_config,
                threads,
            )
            for chunk in chunks
        ]
        for run in as_completed(futures):
            for container, object_id in run.result():
                if object_id:
                    objects_struct.append({'container': container, 'object': object_id})

    elapsed = time.monotonic() - start
    rate = len(objects_struct) / elapsed if elapsed else 0
    print(f"Upload objects to each container: Completed "
          f"({len(objects_struct)} objects in {elapsed:.1f}s, {rate:.0f} obj/s)")

    data = {'containers': container_list, 'objects': objects_struct, 'obj_size': args.size + " Kb"}

    with open(args.out, 'w+') as f:
        json.dump(data, f, ensure_ascii=False, indent=2)

    print("Result:")
    print(f" > Total Containers has been created: {len(container_list)}.")
    print(f" > Total Objects has been created: {len(objects_struct)}.")


if __name__ == "__main__":
    main()
