import hashlib
import os
from dataclasses import dataclass
from typing import BinaryIO

STREAM_BUFFER_BYTES = 64 * 1024

@dataclass
class Digest:
    bytes: int = 0
    hash: str = ""
    lines: int = 0

def stream(r: BinaryIO) -> Digest:
    hasher = hashlib.sha256()
    digest = Digest()
    last = b""
    while True:
        chunk = r.read(STREAM_BUFFER_BYTES)
        if not chunk:
            break
        hasher.update(chunk)
        digest.bytes += len(chunk)
        digest.lines += chunk.count(b"\n")
        last = chunk[-1:]
    
    if digest.bytes > 0 and last != b"\n":
        digest.lines += 1
    
    digest.hash = hasher.hexdigest()
    return digest

def file(path: str) -> Digest:
    with open(path, "rb") as f:
        return stream(f)
