import re

from helpers.cmd import execute_cmd
from helpers.neofs_grpc import upload_object as upload_object_via_grpc


def create_container(endpoint, policy, wallet_file, wallet_config):
    cmd_line = f"neofs-cli --rpc-endpoint {endpoint} container create --wallet {wallet_file} --config {wallet_config} " \
               f" --policy '{policy}' --basic-acl public-read-write --await"

    output, success = execute_cmd(cmd_line)

    if not success:
        print(f" > Container has not been created:\n{output}")
        return False
    else:
        # Regular expression to find the container ID, case-insensitive
        pattern = r"container ID: ([A-Za-z0-9]{43,44})"

        # Search for the pattern in the output text with case-insensitive flag
        match = re.search(pattern, output, re.IGNORECASE)
        if match:
            container_id = match.group(1)
            print(f"Created container: {container_id}")
            return container_id
        else:
            raise ValueError(f"no CID was parsed from command output: \t{output}")


def upload_object(container, payload_filepath, endpoints, wallet_file, wallet_config, start_index=0):
    return upload_object_via_grpc(
        container, payload_filepath, endpoints, wallet_file, wallet_config, start_index
    )


def get_object(cid, oid, endpoint, out_filepath, wallet_file, wallet_config):
    cmd_line = f"neofs-cli object get -r {endpoint} --cid {cid} --oid {oid} --wallet {wallet_file} --config {wallet_config} " \
               f"--file {out_filepath}"

    output, success = execute_cmd(cmd_line)

    if not success:
        print(f" > Failed to get object {output} from container {cid} \r\n"
              f" > Error: {output}")
        return False

    return True


def search_object_by_id(cid, oid, endpoint, wallet_file, wallet_config, ttl=2):
    cmd_line = f"neofs-cli object search --ttl {ttl} -r {endpoint} --cid {cid} --oid {oid} --wallet {wallet_file} --config {wallet_config} "

    output, success = execute_cmd(cmd_line)

    if not success:
        print(f" > Failed to search object {oid} for container {cid} \r\n"
              f" > Error: {output}")
        return False

    re_rst = re.search(r'Found (\d+) objects', output)

    if not re_rst:
        raise Exception("Failed to parce search results")

    return re_rst.group(1)
