from helpers.cmd import execute_cmd


def create_container(endpoint, policy):
    cmd_line = f"neofs-cli --rpc-endpoint {endpoint} container create -g" \
               f" --policy '{policy}' --basic-acl public-read-write --await"

    output, success = execute_cmd(cmd_line)

    if not success:
        print(f" > Container has not been created:\n{output}")
        return False
    else:
        try:
            fst_str = output.split('\n')[0]
        except Exception:
            print(f"Got empty output: {output}")
            return False
        splitted = fst_str.split(": ")
        if len(splitted) != 2:
            raise ValueError(f"no CID was parsed from command output: \t{fst_str}")

        print(f"Created container: {splitted[1]}")

        return splitted[1]


def upload_object(container, payload_filepath, endpoint):
    object_name = ""
    cmd_line = f"neofs-cli --rpc-endpoint {endpoint} object put -g --file {payload_filepath} " \
               f"--cid {container} --no-progress"
    out, success = execute_cmd(cmd_line)

    if not success:
        print(f" > Object {object_name} has not been uploaded:\n{out}")
        return False
    else:
        try:
            # taking second string from command output
            snd_str = out.split('\n')[1]
        except Exception:
            print(f"Got empty input: {out}")
            return False
        splitted = snd_str.split(": ")
        if len(splitted) != 2:
            raise Exception(f"no OID was parsed from command output: \t{snd_str}")
        return splitted[1]


def get_object(cid, oid, endpoint, out_filepath):
    cmd_line = f"neofs-cli object get -r {endpoint} -g --cid {cid} --oid {oid} " \
               f"--file {out_filepath}"

    out, success = execute_cmd(cmd_line)

    if not success:
        print(f" > Failed to get object {oid} from container {cid} \r\n"
              f" > Error: {out}")
        return False

    return True
