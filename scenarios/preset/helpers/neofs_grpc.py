import hashlib
import json
import threading
import time
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed
from time import sleep

import base58
import grpc
import yaml
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives.asymmetric.utils import decode_dss_signature
from neo3.core.cryptography import ECPoint
from neo3.wallet.utils import public_key_to_script_hash, script_hash_to_address
from neo3.wallet.wallet import Wallet

from helpers.protobuf.object import service_pb2 as object_service_pb2
from helpers.protobuf.object import types_pb2 as object_types_pb2
from helpers.protobuf.refs import types_pb2 as refs_types_pb2
from helpers.protobuf.session import service_pb2 as session_service_pb2
from helpers.protobuf.session import types_pb2 as session_types_pb2

_OBJECT_VERBS = (
    session_types_pb2.Verb.OBJECT_PUT,
    session_types_pb2.Verb.OBJECT_GET,
    session_types_pb2.Verb.OBJECT_HEAD,
    session_types_pb2.Verb.OBJECT_SEARCH,
    session_types_pb2.Verb.OBJECT_DELETE,
)

MAX_RETRIES = 5
DELAY_AFTER_FAILURE = 1
API_VERSION_MAJOR = 2
API_VERSION_MINOR = 25
SESSION_EXPIRATION = 2**63 - 1

_channels = {}
_channel_lock = threading.Lock()
_wallet_cache = {}
_wallet_lock = threading.Lock()
_session_cache = {}
_session_lock = threading.Lock()
_payload_cache = {}
_payload_lock = threading.Lock()
_container_ids = {}
_API_VERSION = None


class _Signer:
    def __init__(self, public_key, private_key):
        self.public_key = public_key
        self._key = ec.derive_private_key(int.from_bytes(private_key, "big"), ec.SECP256R1())

    def sign(self, data):
        der = self._key.sign(data, ec.ECDSA(hashes.SHA512()))
        r, s = decode_dss_signature(der)
        signature = refs_types_pb2.Signature()
        signature.key = self.public_key
        signature.sign = b"\x04" + r.to_bytes(32, "big") + s.to_bytes(32, "big")
        signature.scheme = refs_types_pb2.SignatureScheme.ECDSA_SHA512
        return signature


def _verification_header(body_sig, meta_sig):
    verify_header = session_types_pb2.RequestVerificationHeader()
    verify_header.body_signature.CopyFrom(body_sig)
    verify_header.meta_signature.CopyFrom(meta_sig)
    return verify_header


def _load_cli_config(wallet_config):
    if not wallet_config:
        return "", ""
    with open(wallet_config) as f:
        cfg = yaml.safe_load(f) or {}
    return cfg.get("password") or "", cfg.get("address") or ""


def _normalize_wallet_json(data):
    # neo-go / neofs-cli accept NEP-6 wallets without fields neo-mamba requires.
    if "name" not in data:
        data["name"] = ""
    if "extra" not in data:
        data["extra"] = {}
    for account in data.get("accounts") or []:
        account.setdefault("label", None)
        account.setdefault("isDefault", False)
        account.setdefault("lock", False)
        account.setdefault("extra", {})
    return data


def load_wallet_keys(wallet_file, wallet_config):
    cache_key = (wallet_file, wallet_config)
    with _wallet_lock:
        if cache_key in _wallet_cache:
            return _wallet_cache[cache_key]

        password, address = _load_cli_config(wallet_config)
        with open(wallet_file) as f:
            wallet = Wallet.from_json(_normalize_wallet_json(json.load(f)), passwords=[password])
        acc = None
        if address:
            for candidate in wallet.accounts:
                if candidate.address == address:
                    acc = candidate
                    break
            if acc is None:
                raise ValueError(f"account {address} not found in wallet {wallet_file}")
        else:
            acc = wallet.accounts[0]

        public_key = acc.public_key.encode_point(True)
        private_key = acc.private_key
        keys = {
            "address": acc.address,
            "public_key": public_key,
            "private_key": private_key,
            "owner_id": _owner_id(acc.address),
            "signer": _Signer(public_key, private_key),
        }
        _wallet_cache[cache_key] = keys
        return keys


def _channel(endpoint):
    with _channel_lock:
        cached = _channels.get(endpoint)
        if cached:
            return cached
        channel = grpc.insecure_channel(
            endpoint,
            options=(
                ("grpc.keepalive_time_ms", 30000),
                ("grpc.http2.max_concurrent_streams", 64),
            ),
        )
        cached = {
            "channel": channel,
            "put": channel.stream_unary(
                "/neo.fs.v2.object.ObjectService/Put",
                request_serializer=object_service_pb2.PutRequest.SerializeToString,
                response_deserializer=object_service_pb2.PutResponse.FromString,
            ),
            "session_create": channel.unary_unary(
                "/neo.fs.v2.session.SessionService/Create",
                request_serializer=session_service_pb2.CreateRequest.SerializeToString,
                response_deserializer=session_service_pb2.CreateResponse.FromString,
            ),
        }
        _channels[endpoint] = cached
        return cached


def _read_payload(payload_filepath):
    with _payload_lock:
        cached = _payload_cache.get(payload_filepath)
        if cached is None:
            with open(payload_filepath, "rb") as f:
                payload = f.read()
            cached = {
                "payload": payload,
                "sha256": hashlib.sha256(payload).digest(),
            }
            _payload_cache[payload_filepath] = cached
        return cached


def _owner_id(address):
    owner_id = refs_types_pb2.OwnerID()
    owner_id.value = base58.b58decode(address)
    return owner_id


def _owner_id_from_pubkey(pub_key):
    point = ECPoint.deserialize_from_bytes(pub_key)
    return _owner_id(script_hash_to_address(public_key_to_script_hash(point)))


def _api_version():
    global _API_VERSION
    if _API_VERSION is None:
        version = refs_types_pb2.Version()
        version.major = API_VERSION_MAJOR
        version.minor = API_VERSION_MINOR
        _API_VERSION = version
    return _API_VERSION


def _create_session(endpoint, keys):
    request = session_service_pb2.CreateRequest()
    request.body.owner_id.CopyFrom(keys["owner_id"])
    request.body.expiration = SESSION_EXPIRATION

    meta_header = session_types_pb2.RequestMetaHeader()
    meta_header.version.CopyFrom(_api_version())
    meta_header.epoch = 1
    meta_header.ttl = 2
    request.meta_header.CopyFrom(meta_header)
    request.verify_header.CopyFrom(
        _verification_header(
            keys["signer"].sign(request.body.SerializeToString(deterministic=True)),
            keys["signer"].sign(request.meta_header.SerializeToString(deterministic=True)),
        )
    )

    response = _channel(endpoint)["session_create"](request)
    _check_status(response, "session create")
    return response.body.session_key


def _session_token_v2(endpoint, keys):
    cache_key = (endpoint, keys["address"])
    with _session_lock:
        cached = _session_cache.get(cache_key)
        if cached:
            return cached

        session_key = _create_session(endpoint, keys)
        now = int(time.time())
        token = session_types_pb2.SessionTokenV2()
        token.body.version = 0
        token.body.issuer.CopyFrom(keys["owner_id"])
        subject = token.body.subjects.add()
        subject.owner_id.CopyFrom(keys["owner_id"])
        session_subject = token.body.subjects.add()
        session_subject.owner_id.CopyFrom(_owner_id_from_pubkey(session_key))
        token.body.lifetime.iat = now
        token.body.lifetime.nbf = now - 7200
        token.body.lifetime.exp = now + 365 * 24 * 3600
        context = token.body.contexts.add()
        context.verbs.extend(_OBJECT_VERBS)
        token.body.final = True
        token.signature.CopyFrom(keys["signer"].sign(token.body.SerializeToString(deterministic=True)))

        meta_header = session_types_pb2.RequestMetaHeader()
        meta_header.version.CopyFrom(_api_version())
        meta_header.epoch = 1
        meta_header.ttl = 2
        meta_header.session_token_v2.CopyFrom(token)
        cached = {
            "token": token,
            "meta": meta_header,
            "meta_sig": keys["signer"].sign(meta_header.SerializeToString(deterministic=True)),
        }
        _session_cache[cache_key] = cached
        return cached


def _check_status(response, action):
    status = response.meta_header.status
    if status.code:
        raise RuntimeError(f"{action} failed: code={status.code} message={status.message}")


def _object_header(keys, container_id, payload_info):
    header = object_types_pb2.Header()
    header.version.CopyFrom(_api_version())
    header.container_id.CopyFrom(container_id)
    header.owner_id.CopyFrom(keys["owner_id"])
    header.creation_epoch = 1
    header.payload_length = len(payload_info["payload"])

    payload_checksum = refs_types_pb2.Checksum()
    payload_checksum.type = refs_types_pb2.ChecksumType.SHA256
    payload_checksum.sum = payload_info["sha256"]
    header.payload_hash.CopyFrom(payload_checksum)

    header.object_type = object_types_pb2.ObjectType.REGULAR

    filename = object_types_pb2.Header.Attribute()
    filename.key = "FileName"
    filename.value = str(uuid.uuid4())
    header.attributes.append(filename)
    return header


def _chunk_request(payload_info, keys, session):
    with _payload_lock:
        if payload_info.get("chunk_request") is None:
            request = object_service_pb2.PutRequest()
            request.body.chunk = payload_info["payload"]
            request.meta_header.CopyFrom(session["meta"])
            request.verify_header.CopyFrom(
                _verification_header(
                    keys["signer"].sign(request.body.SerializeToString(deterministic=True)),
                    session["meta_sig"],
                )
            )
            payload_info["chunk_request"] = request
        return payload_info["chunk_request"]


def _init_request(header, keys, session):
    request = object_service_pb2.PutRequest()
    request.body.init.header.CopyFrom(header)
    request.meta_header.CopyFrom(session["meta"])
    request.verify_header.CopyFrom(
        _verification_header(
            keys["signer"].sign(request.body.SerializeToString(deterministic=True)),
            session["meta_sig"],
        )
    )
    return request


def _container_id(container):
    cached = _container_ids.get(container)
    if cached is None:
        cached = refs_types_pb2.ContainerID()
        cached.value = base58.b58decode(container)
        _container_ids[container] = cached
    return cached


def _put_object(endpoint, container, payload_info, keys):
    container_id = _container_id(container)
    payload = payload_info["payload"]

    session = _session_token_v2(endpoint, keys)
    header = _object_header(keys, container_id, payload_info)
    init_request = _init_request(header, keys, session)
    chunk_request = _chunk_request(payload_info, keys, session) if payload else None

    def request_stream():
        yield init_request
        if chunk_request is not None:
            yield chunk_request

    response = _channel(endpoint)["put"](request_stream())
    _check_status(response, "object put")
    return base58.b58encode(response.body.object_id.value).decode()


def init_worker(payload_filepath, endpoints, wallet_file, wallet_config):
    keys = load_wallet_keys(wallet_file, wallet_config)
    _read_payload(payload_filepath)
    if isinstance(endpoints, str):
        endpoints = [endpoints]
    for endpoint in endpoints:
        _channel(endpoint)
        _session_token_v2(endpoint, keys)


def worker_ready():
    return True


def upload_objects(jobs, payload_filepath, endpoints, wallet_file, wallet_config, threads=1):
    init_worker(payload_filepath, endpoints, wallet_file, wallet_config)
    if threads <= 1 or len(jobs) <= 1:
        return [
            (container, upload_object(
                container, payload_filepath, endpoints, wallet_file, wallet_config, start_index,
            ))
            for container, start_index in jobs
        ]

    results = []
    with ThreadPoolExecutor(max_workers=threads) as executor:
        futures = {
            executor.submit(
                upload_object,
                container,
                payload_filepath,
                endpoints,
                wallet_file,
                wallet_config,
                start_index,
            ): container
            for container, start_index in jobs
        }
        for fut in as_completed(futures):
            results.append((futures[fut], fut.result()))
    return results


def upload_object(container, payload_filepath, endpoints, wallet_file, wallet_config, start_index=0):
    keys = load_wallet_keys(wallet_file, wallet_config)
    payload_info = _read_payload(payload_filepath)
    if isinstance(endpoints, str):
        endpoints = [endpoints]

    for attempt in range(MAX_RETRIES):
        endpoint = endpoints[(start_index + attempt) % len(endpoints)]
        try:
            return _put_object(endpoint, container, payload_info, keys)
        except Exception as e:
            print(f" > Object has not been uploaded to {container} via {endpoint} "
                  f"({attempt + 1} attempt): {e}")
            if attempt + 1 < MAX_RETRIES:
                sleep(DELAY_AFTER_FAILURE)
    return False
