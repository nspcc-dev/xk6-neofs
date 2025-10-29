import uuid
from time import sleep

from helpers.cmd import execute_cmd


def create_bucket(endpoint, versioning, location):
    bucket_create_marker = False

    if location:
        location = f"--create-bucket-configuration 'LocationConstraint={location}'"
    bucket_name = str(uuid.uuid4())

    cmd_line = f"aws --no-verify-ssl s3api create-bucket --bucket {bucket_name} " \
               f"--endpoint http://{endpoint} {location}"
    cmd_line_ver = f"aws --no-verify-ssl s3api put-bucket-versioning --bucket {bucket_name} " \
                   f"--versioning-configuration Status=Enabled --endpoint http://{endpoint} "

    out, success = execute_cmd(cmd_line)

    if not success:
        if "succeeded and you already own it" in out:
            bucket_create_marker = True
        else:
            print(f" > Bucket {bucket_name} has not been created.")
    else:
        bucket_create_marker = True
        print(f"cmd: {cmd_line}")

    if bucket_create_marker and versioning == "True":
        out, success = execute_cmd(cmd_line_ver)
        if not success:
            print(f" > Bucket versioning has not been applied for bucket {bucket_name}.")
        else:
            print(f" > Bucket versioning has been applied.")

    return bucket_name


def upload_object(bucket, payload_filepath, endpoint):
    MAX_RETRIES_NUMBER = 5
    DELAY_AFTER_FAILURE = 1 # seconds

    object_name = str(uuid.uuid4())

    cmd_line = f"aws s3api put-object --bucket {bucket} --key {object_name} " \
               f"--body {payload_filepath} --endpoint http://{endpoint}"
    for i in range(MAX_RETRIES_NUMBER):
        out, success = execute_cmd(cmd_line)

        if not success:
            print(f" > Object {object_name} has not been uploaded ({i+1} attempt), retrying after {DELAY_AFTER_FAILURE}s...")
            sleep(DELAY_AFTER_FAILURE)
            continue
        else:
            return object_name

    print(f" > Object {object_name} has not been uploaded after {MAX_RETRIES_NUMBER} tries.")
    return False
