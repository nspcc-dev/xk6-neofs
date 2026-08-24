import threading
import uuid
from time import sleep

import boto3
import urllib3
from botocore.config import Config
from botocore.exceptions import BotoCoreError, ClientError

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

_client = None
_client_lock = threading.Lock()


def _endpoint_url(endpoint):
    if endpoint.startswith('http://') or endpoint.startswith('https://'):
        return endpoint
    return f'http://{endpoint}'


def _make_client(endpoint, max_pool_connections):
    session = boto3.session.Session()
    return session.client(
        's3',
        endpoint_url=_endpoint_url(endpoint),
        verify=False,
        config=Config(
            region_name=session.region_name or 'us-east-1',
            retries={'max_attempts': 1, 'mode': 'standard'},
            max_pool_connections=max_pool_connections,
            s3={'addressing_style': 'path'},
            connect_timeout=10,
            read_timeout=120,
            tcp_keepalive=True,
        ),
    )


def init_client(endpoint, max_pool_connections=50):
    global _client
    with _client_lock:
        _client = _make_client(endpoint, max_pool_connections)
        return _client


def _get_client(endpoint):
    if _client is None:
        init_client(endpoint)
    return _client


def create_bucket(endpoint, versioning, location):
    bucket_name = str(uuid.uuid4())
    client = _get_client(endpoint)

    params = {'Bucket': bucket_name}
    if location:
        params['CreateBucketConfiguration'] = {'LocationConstraint': location}

    try:
        client.create_bucket(**params)
    except ClientError as e:
        code = e.response.get('Error', {}).get('Code', '')
        msg = str(e)
        if code not in ('BucketAlreadyOwnedByYou', 'BucketAlreadyExists') and \
                'succeeded and you already own it' not in msg:
            print(f" > Bucket {bucket_name} has not been created: {e}")
            return None
    except (BotoCoreError, Exception) as e:
        print(f" > Bucket {bucket_name} has not been created: {e}")
        return None

    if str(versioning) == 'True':
        try:
            client.put_bucket_versioning(
                Bucket=bucket_name,
                VersioningConfiguration={'Status': 'Enabled'},
            )
            print(' > Bucket versioning has been applied.')
        except (BotoCoreError, ClientError) as e:
            print(f" > Bucket versioning has not been applied for bucket {bucket_name}: {e}")

    return bucket_name


def upload_object(bucket, payload_filepath, endpoint):
    max_retries = 5
    delay_after_failure = 1
    object_name = str(uuid.uuid4())
    client = _get_client(endpoint)

    for attempt in range(max_retries):
        try:
            with open(payload_filepath, 'rb') as body:
                client.put_object(Bucket=bucket, Key=object_name, Body=body)
            return object_name
        except (BotoCoreError, ClientError, OSError) as e:
            print(f" > Object {object_name} has not been uploaded "
                  f"({attempt + 1} attempt): {e}, retrying after {delay_after_failure}s...")
            sleep(delay_after_failure)

    print(f" > Object {object_name} has not been uploaded after {max_retries} tries.")
    return False
