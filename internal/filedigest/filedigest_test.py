import hashlib
import io
import os
import tempfile
import tracemalloc

from .filedigest import stream, file

def test_stream_matches_whole_content_hash_and_line_count():
    cases = {
        "empty": b"",
        "no trailing newline": b"one\ntwo",
        "trailing newline": b"one\ntwo\n",
        "binary": b"\x00\x01\x02\n\x00",
    }
    for name, content in cases.items():
        digest = stream(io.BytesIO(content))
        sum_hash = hashlib.sha256(content).hexdigest()
        assert digest.hash == sum_hash, f"hash = {digest.hash}, want {sum_hash}"
        assert digest.bytes == len(content), f"bytes = {digest.bytes}, want {len(content)}"
        
        want_lines = content.count(b"\n")
        if len(content) > 0 and not content.endswith(b"\n"):
            want_lines += 1
        
        assert digest.lines == want_lines, f"lines = {digest.lines}, want {want_lines}"

def test_file_digests_without_holding_the_file():
    size = 64 << 20
    with tempfile.TemporaryDirectory() as temp_dir:
        path = os.path.join(temp_dir, "huge.bin")
        with open(path, "wb") as f:
            f.write(b"header\n")
            f.truncate(size)
        
        tracemalloc.start()
        digest = file(path)
        current, peak = tracemalloc.get_traced_memory()
        tracemalloc.stop()

        assert digest.bytes == size, f"bytes = {digest.bytes}, want {size}"
        assert peak < 1 << 20, f"digesting a {size}-byte file allocated {peak} bytes, want under 1 MiB"
