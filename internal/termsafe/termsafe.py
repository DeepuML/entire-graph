import io
import unicodedata

KEEP_LAYOUT = 0
ESCAPE_LAYOUT = 1
JSON_LAYOUT = 2

ESCAPE_HEADROOM = 16
ESCAPE_FLUSH_BYTES = 32 << 10

def escaped_flush_capacity(input_bytes: int) -> int:
    worst = input_bytes * 6 + ESCAPE_HEADROOM
    if worst < ESCAPE_FLUSH_BYTES + ESCAPE_HEADROOM:
        return worst
    return ESCAPE_FLUSH_BYTES + ESCAPE_HEADROOM

HEX_DIGITS = b"0123456789abcdef"

def rune_width_at(data: bytes, i: int) -> int:
    lead = data[i]
    if lead < 0x80:
        return 1
    elif lead < 0xc2:
        return 0
    elif lead < 0xe0:
        return sequence_width(data, i, 2, 0x80, 0xbf)
    elif lead == 0xe0:
        return sequence_width(data, i, 3, 0xa0, 0xbf)
    elif lead == 0xed:
        return sequence_width(data, i, 3, 0x80, 0x9f)
    elif lead < 0xf0:
        return sequence_width(data, i, 3, 0x80, 0xbf)
    elif lead == 0xf0:
        return sequence_width(data, i, 4, 0x90, 0xbf)
    elif lead == 0xf4:
        return sequence_width(data, i, 4, 0x80, 0x8f)
    elif lead < 0xf4:
        return sequence_width(data, i, 4, 0x80, 0xbf)
    return 0

def sequence_width(data: bytes, i: int, width: int, second_low: int, second_high: int) -> int:
    if i + width > len(data):
        return 0
    if not (second_low <= data[i+1] <= second_high):
        return 0
    for offset in range(2, width):
        if not (0x80 <= data[i+offset] <= 0xbf):
            return 0
    return width

def decodepoint(data: bytes, i: int, width: int) -> int:
    if width == 2:
        return ((data[i] & 0x1f) << 6) | (data[i+1] & 0x3f)
    elif width == 3:
        return ((data[i] & 0x0f) << 12) | ((data[i+1] & 0x3f) << 6) | (data[i+2] & 0x3f)
    elif width == 4:
        return ((data[i] & 0x07) << 18) | ((data[i+1] & 0x3f) << 12) | ((data[i+2] & 0x3f) << 6) | (data[i+3] & 0x3f)
    return data[i]

def separates_lines(point: int) -> bool:
    try:
        cat = unicodedata.category(chr(point))
        return cat in ("Zl", "Zp")
    except ValueError:
        return False

# Bidi_Control characters
BIDI_CONTROLS = {
    0x061C, 0x200E, 0x200F,
    0x202A, 0x202B, 0x202C, 0x202D, 0x202E,
    0x2066, 0x2067, 0x2068, 0x2069
}

def reorders_row(point: int) -> bool:
    return point in BIDI_CONTROLS

def forges_record_row(point: int) -> bool:
    return separates_lines(point) or reorders_row(point)

def escaped_at(data: bytes, i: int, keep: int) -> tuple[int, bool]:
    char = data[i]
    if keep == JSON_LAYOUT and char < 0x80:
        return 1, False
    
    if char in (b'\n'[0], b'\t'[0], b'\f'[0], b'\v'[0]):
        return 1, keep != KEEP_LAYOUT
    
    if char == b'\r'[0]:
        if keep == KEEP_LAYOUT and i + 1 < len(data) and data[i+1] == b'\n'[0]:
            return 1, False
        return 1, True
    
    if char < 0x20 or char == 0x7f:
        return 1, True
    
    if char < 0x80:
        return 1, False

    width = rune_width_at(data, i)
    if width == 2 and char == 0xc2 and 0x80 <= data[i+1] <= 0x9f:
        return 2, True
    elif width > 0:
        return width, keep == ESCAPE_LAYOUT and forges_record_row(decodepoint(data, i, width))
    elif char <= 0x9f:
        return 1, True
    return 1, False

def needs_escape(data: bytes, keep: int) -> bool:
    i = 0
    while i < len(data):
        width, escape = escaped_at(data, i, keep)
        if escape:
            return True
        i += width
    return False

def append_point_escape(point: int) -> bytes:
    if point > 0xffff:
        return b"\\U00" + bytes([
            HEX_DIGITS[(point >> 20) & 0x0f],
            HEX_DIGITS[(point >> 16) & 0x0f],
            HEX_DIGITS[(point >> 12) & 0x0f],
            HEX_DIGITS[(point >> 8) & 0x0f],
            HEX_DIGITS[(point >> 4) & 0x0f],
            HEX_DIGITS[point & 0x0f]
        ])
    return b"\\u" + bytes([
        HEX_DIGITS[(point >> 12) & 0x0f],
        HEX_DIGITS[(point >> 8) & 0x0f],
        HEX_DIGITS[(point >> 4) & 0x0f],
        HEX_DIGITS[point & 0x0f]
    ])

def append_escaped_at(dst: bytearray, data: bytes, i: int, width: int, escape: bool):
    if not escape:
        dst.extend(data[i:i+width])
    elif width == 1 and data[i] < 0x80:
        dst.extend(b"\\x" + bytes([HEX_DIGITS[data[i] >> 4], HEX_DIGITS[data[i] & 0x0f]]))
    else:
        dst.extend(append_point_escape(decodepoint(data, i, width)))

def append_escaped(data: bytes, keep: int) -> bytes:
    dst = bytearray()
    i = 0
    while i < len(data):
        width, escape = escaped_at(data, i, keep)
        append_escaped_at(dst, data, i, width, escape)
        i += width
    return bytes(dst)

def flush_escaped(out, buffer: bytearray):
    if not buffer:
        return
    written = out.write(buffer)
    if written != len(buffer):
        raise IOError("Short write")

def write_escaped(out, p: bytes, keep: int) -> int:
    if not needs_escape(p, keep):
        return out.write(p)
    
    buffer = bytearray()
    i = 0
    while i < len(p):
        width, escape = escaped_at(p, i, keep)
        append_escaped_at(buffer, p, i, width, escape)
        i += width
        
        if len(buffer) < ESCAPE_FLUSH_BYTES:
            continue
            
        flush_escaped(out, buffer)
        buffer.clear()
        
    flush_escaped(out, buffer)
    return len(p)

class Writer:
    def __init__(self, out):
        self.out = out
        
    def write(self, p: bytes) -> int:
        write_escaped(self.out, p, KEEP_LAYOUT)
        return len(p)

class JSONWriter:
    def __init__(self, out):
        self.out = out
        self.pending_lead = False
        
    def write(self, p: bytes) -> int:
        total = len(p)
        rest = p
        
        if self.pending_lead:
            self.pending_lead = False
            if not rest:
                self.pending_lead = True
                return total
                
            pair = bytes([0xc2, rest[0]])
            width, escape = escaped_at(pair, 0, JSON_LAYOUT)
            
            buf = bytearray()
            append_escaped_at(buf, pair, 0, width, escape)
            flush_escaped(self.out, buf)
            
            if width == 2:
                rest = rest[1:]
                
        withheld_lead = len(rest) > 0 and rest[-1] == 0xc2
        if withheld_lead:
            rest = rest[:-1]
            
        write_escaped(self.out, rest, JSON_LAYOUT)
        self.pending_lead = withheld_lead
        return total
        
    def close(self):
        if not self.pending_lead:
            return
        self.pending_lead = False
        self.out.write(b"\xc2")

def line(value: str) -> str:
    b = value.encode('utf-8')
    if not needs_escape(b, ESCAPE_LAYOUT):
        return value
    return append_escaped(b, ESCAPE_LAYOUT).decode('utf-8')

def escapes_line(value: str) -> bool:
    return needs_escape(value.encode('utf-8'), ESCAPE_LAYOUT)

def bytes_safe(value: bytes) -> bytes:
    if not needs_escape(value, KEEP_LAYOUT):
        return value
    return append_escaped(value, KEEP_LAYOUT)
