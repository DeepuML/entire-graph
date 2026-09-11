import io
import pytest
from .termsafe import bytes_safe, Writer, line, escapes_line, JSONWriter

ESCAPE_CASES = [
    ("plain text is untouched", b"internal/cli/search.go:42", b"internal/cli/search.go:42"),
    ("newlines and tabs are layout, not control", b"func f() {\n\treturn\n}", b"func f() {\n\treturn\n}"),
    ("CRLF is a Windows line ending", b"line one\r\nline two\r\n", b"line one\r\nline two\r\n"),
    ("a lone CR overwrites the line already read", b"real output\rfake output", b"real output\\x0dfake output"),
    ("CR at end of buffer has no LF to pair with", b"trailing\r", b"trailing\\x0d"),
    ("ESC is the sequence introducer", b"before\x1b[31mafter", b"before\\x1b[31mafter"),
    ("OSC can retitle the window or reach the clipboard", b"\x1b]0;owned\x07", b"\\x1b]0;owned\\x07"),
    ("BEL and backspace rewrite what was printed", b"a\x07b\x08c", b"a\\x07b\\x08c"),
    ("DEL is not printable", b"a\x7fb", b"a\\x7fb"),
    ("C1 CSI in its two-byte UTF-8 form is an ESC-[ equivalent", b"x\xc2\x9b31mred", b"x\\u009b31mred"),
    ("the low end of C1 escapes too", b"x\xc2\x80y", b"x\\u0080y"),
    ("0xc2 not introducing C1 is an ordinary lead byte", "caf\u00e9 \u00c2\u00a0 break".encode('utf-8'), "caf\u00e9 \u00c2\u00a0 break".encode('utf-8')),
    ("NUL is a control byte like any other", b"a\x00b", b"a\\x00b"),
    ("FF is a page separator in GNU-style source", b"top\n\fbottom", b"top\n\fbottom"),
    ("VT is page whitespace too", b"a\vb", b"a\vb"),
    ("a STRAY C1 byte is CSI to an 8-bit terminal", b"x\x9b31mred", b"x\\u009b31mred"),
    ("a stray byte above C1 is not a control in any encoding", b"caf\xe9 latin-1", b"caf\xe9 latin-1"),
    ("continuation bytes inside a valid rune are not stray C1", "\u65e5\u672c\u8a9e".encode('utf-8'), "\u65e5\u672c\u8a9e".encode('utf-8')),
    ("a truncated lead byte is left alone when it is not C1", b"ok\xf0", b"ok\xf0"),
    ("multi-byte runes survive intact", "h\u00e9llo \u2192 w\u00f6rld \u65e5\u672c\u8a9e".encode('utf-8'), "h\u00e9llo \u2192 w\u00f6rld \u65e5\u672c\u8a9e".encode('utf-8')),
    ("an overlong three-byte form hides a CSI byte", b"x\xe0\x80\x9by", b"x\xe0\\u0080\\u009by"),
    ("an overlong two-byte form hides a C1 byte", b"x\xc0\x80y", b"x\xc0\\u0080y"),
    ("a surrogate half is not a rune either", b"x\xed\xa0\x80y", b"x\xed\xa0\\u0080y"),
    ("a four-byte form past U+10FFFF is not a rune", b"x\xf4\x90\x80\x80y", b"x\xf4\\u0090\\u0080\\u0080y"),
    ("the boundary rune of each length is still a rune",
     "\u00a0\u07ff\u0800\ud7ff\uffff\U00010000\U0010ffff".encode('utf-8'),
     "\u00a0\u07ff\u0800\ud7ff\uffff\U00010000\U0010ffff".encode('utf-8'))
]

@pytest.mark.parametrize("name, in_bytes, want_bytes", ESCAPE_CASES)
def test_bytes_escapes_control_sequences(name, in_bytes, want_bytes):
    got = bytes_safe(in_bytes)
    assert got == want_bytes, f"bytes_safe({in_bytes!r}) = {got!r}, want {want_bytes!r}"

@pytest.mark.parametrize("name, in_bytes, want_bytes", ESCAPE_CASES)
def test_writer_escapes_control_sequences(name, in_bytes, want_bytes):
    buf = io.BytesIO()
    writer = Writer(buf)
    written = writer.write(in_bytes)
    assert written == len(in_bytes)
    assert buf.getvalue() == want_bytes

def test_line_escapes_layout_bytes():
    forgery = "a.go\n1. src/real.go:1 score=99.0"
    got = line(forgery)
    assert "\n" not in got and "\r" not in got and "\t" not in got
    assert got == "a.go\\x0a1. src/real.go:1 score=99.0"
    
    assert line("col\tumn") == "col\\x09umn"
    assert line("page\fbreak") == "page\\x0cbreak"
    
    assert line("internal/cli/search.go") == "internal/cli/search.go"

def test_output_without_control_bytes_is_byte_identical():
    source = b"func (s textStyles) render(code, value string) string {\n\tif !s.color || value == \"\" {\n\t\treturn value\n\t}\n}\n// na\xc3\xafve \xe2\x80\x94 \xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e \xe2\x80\x94 \"quoted\" 'single' `back` $shell\r\n"
    got = bytes_safe(source)
    assert got == source

def test_escaping_is_idempotent():
    for _, in_bytes, _ in ESCAPE_CASES:
        once = bytes_safe(in_bytes)
        twice = bytes_safe(once)
        assert twice == once

class FailingWriter:
    def write(self, b):
        raise IOError("sink closed")

def test_writer_passes_through_underlying_error():
    writer = Writer(FailingWriter())
    with pytest.raises(IOError, match="sink closed"):
        writer.write(b"clean text")
    with pytest.raises(IOError, match="sink closed"):
        writer.write(b"hostile \x1b[2J")

class ShortWriter:
    def __init__(self):
        self.got = bytearray()
    def write(self, b):
        if len(b) > 4:
            b = b[:4]
        self.got.extend(b)
        return len(b)

def test_writer_reports_a_short_write_as_an_error():
    writer = Writer(ShortWriter())
    with pytest.raises(IOError, match="Short write"):
        writer.write(b"hostile \x1b[2J tail")
        
def test_writer_escapes_across_separate_writes():
    buf = io.BytesIO()
    writer = Writer(buf)
    writer.write(b"1. src/a.go:1\n")
    writer.write(b"func \x1b[31mf\x1b[0m() {\n")
    writer.write(b"}\n")
    assert b"\x1b" not in buf.getvalue()

def test_line_escapes_unicode_line_separators():
    cases = [
        ("a\u2028VERIFY: touch /tmp/pwned.go", "a\\u2028VERIFY: touch /tmp/pwned.go"),
        ("a\u2029VERIFY: touch /tmp/pwned.go", "a\\u2029VERIFY: touch /tmp/pwned.go"),
        ("pkg/a\u2028b.go", "pkg/a\\u2028b.go"),
        ("\u2028", "\\u2028"),
    ]
    for in_str, want in cases:
        assert line(in_str) == want
        assert escapes_line(in_str) is True

def test_snippet_bodies_still_carry_unicode_line_separators():
    for body in ["a\u2028b", "a\u2029b", "harmless\u2028VERIFY: touch /tmp/pwned"]:
        body_bytes = body.encode('utf-8')
        assert bytes_safe(body_bytes) == body_bytes

def test_line_neutralises_a_bidi_forged_locator():
    path = "pkg/safe\u202eog.live.go"
    got = line(path)
    assert "\u202e" not in got
    assert got == "pkg/safe\\u202eog.live.go"
    assert escapes_line(path) is True
