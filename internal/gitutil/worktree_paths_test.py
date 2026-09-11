import io
import os
import sys
import time
import pytest
import platform
import subprocess
import tracemalloc
from typing import List, Set

BOUNDED_PATH_OUTPUT_HELPER_ENV = "ENTIRE_GRAPH_TEST_BOUNDED_PATH_OUTPUT_HELPER"

# Helper process logic must execute at import time or file run time if the helper env is set.
if os.getenv(BOUNDED_PATH_OUTPUT_HELPER_ENV) == "1":
    record = b"src.go/unexpected-staged-descendant.go\x00"
    emitted = 0
    try:
        # Write to stdout using sys.stdout.buffer
        while emitted <= (8 << 20):
            sys.stdout.buffer.write(record)
            sys.stdout.buffer.flush()
            emitted += len(record)
    except Exception:
        pass
    time.sleep(3600)
    sys.exit(0)

from .worktree_paths import (
    LITERAL_PATHSPEC_BATCH_COUNT,
    LITERAL_PATHSPEC_BATCH_BYTES,
    NESTED_IGNORE_CANDIDATE_MAX_COUNT,
    literal_pathspec_batch_end,
    ClassifyWorktreePaths,
    IndexHasFilesUnder,
    TreeContainsPaths,
    FirstTreeNestedIgnorePaths,
    FirstWorktreeNestedIgnorePaths,
    BoundedWorktreeNestedIgnorePaths,
    BoundedTreeNestedIgnorePaths,
    VisitWorktreePaths,
    run_bounded_path_output,
    new_cmd,
)

def git_cmd(repo: str, *args: str) -> str:
    env = os.environ.copy()
    res = subprocess.run(
        ["git"] + list(args),
        cwd=repo,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=True,
        env=env
    )
    return res.stdout

def write_file(repo: str, rel_path: str, content: str) -> None:
    full_path = os.path.join(repo, rel_path)
    os.makedirs(os.path.dirname(full_path), exist_ok=True)
    with open(full_path, "w", encoding="utf-8") as f:
        f.write(content)

def git_input_output(repo: str, stdin_content: str, *args: str) -> str:
    res = subprocess.run(
        ["git"] + list(args),
        cwd=repo,
        input=stdin_content,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=True
    )
    return res.stdout.strip()


def test_literal_pathspec_batch_end_leaves_windows_command_line_headroom():
    literal_prefix = ":(literal)"
    windows_command_line_max = 32767
    fixed_argument_headroom = 1024

    worst_escaped_units = (
        2 * LITERAL_PATHSPEC_BATCH_BYTES
        + 2 * LITERAL_PATHSPEC_BATCH_COUNT
        + fixed_argument_headroom
    )
    assert worst_escaped_units < windows_command_line_max, (
        f"worst-case escaped batch uses {worst_escaped_units} UTF-16 units, want less than {windows_command_line_max}"
    )

    second = "b"
    first_at_limit = "a" * (
        LITERAL_PATHSPEC_BATCH_BYTES - 2 * len(literal_prefix.encode('utf-8')) - len(second.encode('utf-8'))
    )
    assert literal_pathspec_batch_end([first_at_limit, second], 0) == 2

    first_over_limit = first_at_limit + "a"
    assert literal_pathspec_batch_end([first_over_limit, second], 0) == 1

    paths = [None] * (LITERAL_PATHSPEC_BATCH_COUNT + 1)
    assert literal_pathspec_batch_end(paths, 0) == LITERAL_PATHSPEC_BATCH_COUNT


def test_classify_worktree_paths_bounds_staged_subtree_conflict(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")
    write_file(repo, "src.go/staged.go", "package staged\n")
    git_cmd(repo, "add", "src.go/staged.go")
    
    staged_dir = os.path.join(repo, "src.go")
    moved_dir = str(tmp_path / "staged-src.go")
    os.rename(staged_dir, moved_dir)
    
    write_file(repo, "src.go", "package src\n")

    with pytest.raises(Exception) as excinfo:
        ClassifyWorktreePaths(None, repo, ["src.go"])
    assert "unexpected path" in str(excinfo.value)


def test_run_bounded_path_output_rejects_unexpected_streaming_output():
    cmd = new_cmd(None, "", sys.executable, os.path.abspath(__file__))
    cmd.env[BOUNDED_PATH_OUTPUT_HELPER_ENV] = "1"
    
    tracemalloc.start()
    try:
        start_time = time.time()
        with pytest.raises(Exception) as excinfo:
            run_bounded_path_output(cmd, {"src.go"})
        elapsed = time.time() - start_time
        
        # Ensure process was reaped/terminated
        assert cmd.process is not None
        assert cmd.process.poll() is not None
        
        assert "unexpected path" in str(excinfo.value)
        assert elapsed < 5.0, f"took {elapsed}s to reject and reap helper, want less than 5s"
        
        current, peak = tracemalloc.get_traced_memory()
        assert peak < (4 << 20), f"allocated {peak} bytes, want at most {4<<20}"
    finally:
        tracemalloc.stop()


def test_classify_worktree_paths_uses_effective_excludes_and_literal_names(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")
    git_cmd(repo, "config", "user.name", "T")
    git_cmd(repo, "config", "user.email", "t@example.com")
    write_file(repo, "nested/.gitignore", "*.generated\n")
    write_file(repo, "global-excludes", "*.private\n")
    git_cmd(repo, "config", "core.excludesFile", os.path.join(repo, "global-excludes"))
    write_file(repo, ".git/info/exclude", "*.local-only\n")
    write_file(repo, "nested/[literal].generated", "generated\n")
    write_file(repo, "nested/l.generated", "competing glob match\n")
    write_file(repo, "nested/also.private", "private\n")
    write_file(repo, "nested/also.local-only", "local\n")
    write_file(repo, "nested/keep.go", "package keep\n")

    eligible, classified_ignored = ClassifyWorktreePaths(None, repo, [
        "nested/[literal].generated",
        "nested/also.private",
        "nested/also.local-only",
        "nested/keep.go",
        ".git/config",
    ])

    assert "nested/keep.go" in eligible
    for path in ["nested/[literal].generated", "nested/also.local-only"]:
        assert path in classified_ignored, f"ignored worktree file {path} was not classified: {classified_ignored}"

    assert "nested/also.private" in eligible, f"configuration-derived core.excludesFile unexpectedly affected classification: {eligible}"
    assert ".git/config" not in eligible
    assert ".git/config" not in classified_ignored


def test_index_has_files_under_uses_literal_pathspec(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")
    git_cmd(repo, "config", "user.name", "T")
    git_cmd(repo, "config", "user.email", "t@example.com")
    write_file(repo, "src/build/a.go", "package build\n")
    write_file(repo, "vendor/[tracked]/a.go", "package tracked\n")
    write_file(repo, "vendor/m/a.go", "package m\n")
    write_file(repo, "build", "tracked file named build\n")
    git_cmd(repo, "add", ".")
    git_cmd(repo, "commit", "-m", "tracked")

    test_cases = [
        ("src/build", True),
        ("build", False),
        ("vendor/[tracked]", True),
        ("vendor/[m]", False),
    ]

    for path, want in test_cases:
        got = IndexHasFilesUnder(None, repo, path)
        assert got == want, f"IndexHasFilesUnder({path!r}) = {got}, want {want}"

    assert os.path.exists(os.path.join(repo, "vendor", "[tracked]", "a.go"))


def test_index_has_files_under_rejects_directory_file_conflict_exact_file(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")
    git_cmd(repo, "config", "user.name", "T")
    git_cmd(repo, "config", "user.email", "t@example.com")
    write_file(repo, "build/tracked.py", "def tracked():\n    return 'tracked'\n")
    git_cmd(repo, "add", "build/tracked.py")
    git_cmd(repo, "commit", "-m", "tracked build directory")

    got = IndexHasFilesUnder(None, repo, "build")
    assert got is True, "tracked descendant under build/ was not detected"

    write_file(repo, "build/secret.py", "def secret():\n    return 'secret'\n")
    blob = git_input_output(repo, "tracked file named build\n", "hash-object", "-w", "--stdin")
    git_cmd(repo, "rm", "--cached", "-q", "build/tracked.py")
    git_cmd(repo, "update-index", "--add", "--cacheinfo", "100644", blob, "build")

    got = IndexHasFilesUnder(None, repo, "build")
    assert got is False, "exact indexed file build was treated as a tracked descendant of build/"
    assert os.path.exists(os.path.join(repo, "build", "secret.py"))


def test_tree_contains_paths_and_nested_ignore_order(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")
    git_cmd(repo, "config", "user.name", "T")
    git_cmd(repo, "config", "user.email", "t@example.com")
    write_file(repo, ".gitignore", "root only\n")
    write_file(repo, "[literal].go", "package literal\n")
    write_file(repo, "l.go", "package competitor\n")
    write_file(repo, "nested/.gitignore", "first\n")
    write_file(repo, "nested/deep/.gitignore", "second\n")
    git_cmd(repo, "add", "-f", ".")
    git_cmd(repo, "commit", "-m", "tree")

    members = TreeContainsPaths(None, repo, "HEAD", [
        "[literal].go",
        "nested",
        "missing.go",
    ])
    assert "[literal].go" in members, f"literal tree member was absent: {members}"
    assert "missing.go" not in members, f"missing tree path was reported present: {members}"
    assert "nested" not in members, f"tree directory was reported as a file member: {members}"

    ignores = FirstTreeNestedIgnorePaths(None, repo, "HEAD", 1)
    assert ignores == ["nested/.gitignore"], f"first nested HEAD ignore paths = {ignores}, want ['nested/.gitignore']"


def test_tree_contains_paths_supports_newline_path_from_tree_object(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")
    blob = git_input_output(repo, "package newline\n", "hash-object", "-w", "--stdin")
    
    mktree_input = f"100644 blob {blob}\tline\nbreak.go\x00"
    
    res = subprocess.run(
        ["git", "mktree", "-z"],
        cwd=repo,
        input=mktree_input.encode('utf-8'),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=True
    )
    tree = res.stdout.decode('utf-8').strip()

    members = TreeContainsPaths(None, repo, tree, ["line\nbreak.go"])
    assert "line\nbreak.go" in members, f"newline tree member was absent: {members}"


def test_tree_helpers_use_the_repo_subdirectory_path_basis(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")
    git_cmd(repo, "config", "user.name", "T")
    git_cmd(repo, "config", "user.email", "t@example.com")
    write_file(repo, "outside.go", "package outside\n")
    write_file(repo, "scope/[literal].go", "package literal\n")
    write_file(repo, "scope/l.go", "package competitor\n")
    write_file(repo, "scope/nested/.gitignore", "*.generated\n")
    git_cmd(repo, "add", "-f", ".")
    git_cmd(repo, "commit", "-m", "tree")
    scope = os.path.join(repo, "scope")

    members = TreeContainsPaths(None, scope, "HEAD", [
        "[literal].go",
        "outside.go",
    ])
    assert "[literal].go" in members, f"subdirectory-relative literal member was absent: {members}"
    assert "outside.go" not in members, f"path outside the --repo subdirectory was admitted: {members}"

    ignores = FirstTreeNestedIgnorePaths(None, scope, "HEAD", 1)
    assert ignores == ["nested/.gitignore"], f"subdirectory nested ignore paths = {ignores}, want ['nested/.gitignore']"


def test_first_tree_nested_ignore_paths_does_not_cap_ordinary_tree_files(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")

    empty_blob = git_input_output(repo, "", "hash-object", "-w", "--stdin")
    nested_tree = git_input_output(repo, f"100644 blob {empty_blob}\t.gitignore\x00", "mktree", "-z")
    
    ordinary_files = (1 << 16) + 1
    chunks = []
    for index in range(ordinary_files):
        chunks.append(f"100644 blob {empty_blob}\ta{index:05d}.go\x00")
    chunks.append(f"040000 tree {nested_tree}\tnested\x00")
    tree_input = "".join(chunks)
    
    tree = git_input_output(repo, tree_input, "mktree", "-z")

    ignores = FirstTreeNestedIgnorePaths(None, repo, tree, 1)
    assert ignores == ["nested/.gitignore"], f"large-tree nested ignore paths = {ignores}, want ['nested/.gitignore']"
    
    with pytest.raises(ValueError):
        FirstTreeNestedIgnorePaths(None, repo, tree, NESTED_IGNORE_CANDIDATE_MAX_COUNT + 1)


def test_first_worktree_nested_ignore_paths_preserves_provider_order(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")
    git_cmd(repo, "config", "user.name", "T")
    git_cmd(repo, "config", "user.email", "t@example.com")
    write_file(repo, "b/.gitignore", "tracked\n")
    git_cmd(repo, "add", "b/.gitignore")
    git_cmd(repo, "commit", "-m", "tracked")
    write_file(repo, "a/.gitignore", "untracked\n")
    write_file(repo, ".git/info/exclude", "ignored/.gitignore\n")
    write_file(repo, "ignored/.gitignore", "ignored\n")

    without_includes = FirstWorktreeNestedIgnorePaths(None, repo, 3, None)
    assert without_includes == ["a/.gitignore", "b/.gitignore"], f"worktree nested ignore paths without explicit includes = {without_includes}"

    paths = FirstWorktreeNestedIgnorePaths(None, repo, 3, lambda path: path == "ignored/.gitignore")
    assert paths == ["a/.gitignore", "b/.gitignore", "ignored/.gitignore"], f"worktree nested ignore paths = {paths}"


def test_bounded_worktree_nested_ignore_paths_collapse_wholly_ignored_directories(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")
    git_cmd(repo, "config", "user.name", "T")
    git_cmd(repo, "config", "user.email", "t@example.com")
    write_file(repo, ".gitignore", "ignored/\n")
    git_cmd(repo, "add", ".gitignore")
    git_cmd(repo, "commit", "-m", "ignore subtree")
    
    for index in range(NESTED_IGNORE_CANDIDATE_MAX_COUNT + 1):
        write_file(repo, f"ignored/d{index:03d}/.gitignore", "# irrelevant\n")

    paths = BoundedWorktreeNestedIgnorePaths(
        None, repo, NESTED_IGNORE_CANDIDATE_MAX_COUNT, lambda _: True
    )
    assert len(paths) == 0, f"wholly ignored subtree produced nested policy paths {paths}"


def test_bounded_nested_ignore_paths_report_overflow(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")
    git_cmd(repo, "config", "user.name", "T")
    git_cmd(repo, "config", "user.email", "t@example.com")
    for name in ["a/.gitignore", "b/.gitignore", "c/.gitignore"]:
        write_file(repo, name, "# policy\n")
    git_cmd(repo, "add", ".")
    git_cmd(repo, "commit", "-m", "nested ignores")

    with pytest.raises(Exception) as excinfo1:
        BoundedTreeNestedIgnorePaths(None, repo, "HEAD", 2)
    assert "exceed 2 paths" in str(excinfo1.value)

    with pytest.raises(Exception) as excinfo2:
        BoundedWorktreeNestedIgnorePaths(None, repo, 2, None)
    assert "exceed 2 paths" in str(excinfo2.value)

    funcs = [
        lambda: BoundedTreeNestedIgnorePaths(None, repo, "HEAD", 3),
        lambda: BoundedWorktreeNestedIgnorePaths(None, repo, 3, None),
    ]

    for fn in funcs:
        paths = fn()
        assert paths == ["a/.gitignore", "b/.gitignore", "c/.gitignore"], f"bounded nested-ignore paths = {paths}"


def test_bounded_worktree_nested_ignore_paths_deduplicates_unmerged_stages(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")
    git_cmd(repo, "config", "user.name", "T")
    git_cmd(repo, "config", "user.email", "t@example.com")
    
    candidate = "odd\nname/.gitignore"
    if platform.system() == "Windows":
        candidate = "ordinary/name/.gitignore"
        
    write_file(repo, candidate, "# worktree policy\n")
    git_cmd(repo, "add", candidate)
    git_cmd(repo, "commit", "-m", "nested ignore")

    blobs = [
        git_input_output(repo, "# base\n", "hash-object", "-w", "--stdin"),
        git_input_output(repo, "# ours\n", "hash-object", "-w", "--stdin"),
        git_input_output(repo, "# theirs\n", "hash-object", "-w", "--stdin"),
    ]
    git_cmd(repo, "update-index", "--force-remove", "--", candidate)
    
    index_info_chunks = []
    for idx, blob in enumerate(blobs):
        index_info_chunks.append(f"100644 {blob} {idx+1}\t{candidate}\x00")
    index_info = "".join(index_info_chunks)
    git_input_output(repo, index_info, "update-index", "-z", "--index-info")

    res = subprocess.run(
        ["git", "ls-files", "-z", "--cached", "--", ":(glob)**/.gitignore"],
        cwd=repo,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=True
    )
    out = res.stdout.decode('utf-8', errors='surrogateescape')
    assert out.count(candidate + "\x00") == 3, f"unmerged fixture emitted {out.count(candidate + chr(0))} copies, want 3"

    paths = BoundedWorktreeNestedIgnorePaths(None, repo, 1, None)
    assert paths == [candidate], f"bounded unmerged nested-ignore paths = {paths}, want {[candidate]}"


def test_visit_worktree_paths_streams_and_stops_at_visitor_bound(tmp_path):
    repo = str(tmp_path / "repo")
    os.makedirs(repo, exist_ok=True)
    git_cmd(repo, "init")
    for index in range(20):
        write_file(repo, f"p-{index:02d}.go", "package sample\n")

    got = []
    def visitor(path: str) -> bool:
        got.append(path)
        return len(got) < 3

    VisitWorktreePaths(None, repo, False, visitor)
    assert got == ["p-00.go", "p-01.go", "p-02.go"], f"bounded streamed worktree paths = {got}"
