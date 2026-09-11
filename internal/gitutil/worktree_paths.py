import io
import os
import subprocess
import time
from dataclasses import dataclass
from typing import List, Dict, Optional, Tuple, Set

LITERAL_PATHSPEC_BATCH_COUNT = 128
LITERAL_PATHSPEC_BATCH_BYTES = 15 << 10
LITERAL_PATH_OUTPUT_MAX_PATH_BYTES = 4096
NESTED_IGNORE_PATH_MAX_BYTES = 4096
NESTED_IGNORE_CANDIDATE_MAX_COUNT = 512
NESTED_IGNORE_CANDIDATE_MAX_ALL_BYTES = NESTED_IGNORE_CANDIDATE_MAX_COUNT * NESTED_IGNORE_PATH_MAX_BYTES

MAX_WORKTREE_LISTING_FIELDS = 1_000_000
MAX_WORKTREE_LISTING_BYTES = 256 << 20

MAX_IGNORED_DIRECTORY_FIELDS = 2_000_000
MAX_IGNORED_DIRECTORY_BYTES = 64 << 20

class WorktreeListingTruncatedError(Exception):
    def __init__(self, message="Git worktree listing exceeded a raw-output bound"):
        super().__init__(message)

class IgnoredListingTruncatedError(Exception):
    def __init__(self, message="ignored-directory listing exceeded a raw-output bound"):
        super().__init__(message)

@dataclass
class WorktreeListingBudget:
    fields: int = 0
    bytes_count: int = 0

    def admit(self, path: str) -> bool:
        if self.fields >= MAX_WORKTREE_LISTING_FIELDS:
            return False
        record_bytes = len(path.encode('utf-8')) + 1
        if record_bytes > MAX_WORKTREE_LISTING_BYTES - self.bytes_count:
            return False
        self.fields += 1
        self.bytes_count += record_bytes
        return True

@dataclass
class IgnoredDirectoryListingBudget:
    fields: int = 0
    bytes_count: int = 0

    def admit(self, field: str) -> bool:
        if self.fields >= MAX_IGNORED_DIRECTORY_FIELDS:
            return False
        record_bytes = len(field.encode('utf-8')) + 1
        if record_bytes > MAX_IGNORED_DIRECTORY_BYTES - self.bytes_count:
            return False
        self.fields += 1
        self.bytes_count += record_bytes
        return True

class Cmd:
    def __init__(self, args: List[str], dir: str = "", env: Optional[Dict[str, str]] = None):
        self.args = args
        self.dir = dir if dir else None
        self.env = env if env is not None else os.environ.copy()
        self.Stdout = None
        self.Stderr = None
        self.process = None
        self.WaitDelay = 1.0

    def StdoutPipe(self):
        return self

    def Start(self):
        stdout_dest = subprocess.PIPE
        stderr_dest = subprocess.PIPE
        self.process = subprocess.Popen(
            self.args,
            cwd=self.dir,
            env=self.env,
            stdout=stdout_dest,
            stderr=stderr_dest,
        )

    def Run(self):
        self.Start()
        stdout_data, stderr_data = self.process.communicate()
        if self.Stdout is not None and self.Stdout != self:
            self.Stdout.write(stdout_data)
        if self.Stderr is not None:
            self.Stderr.write(stderr_data)
        if self.process.returncode != 0:
            raise subprocess.CalledProcessError(self.process.returncode, self.args, stderr=stderr_data)

    def Wait(self):
        if self.process is None:
            raise RuntimeError("process not started")
        
        stderr_data = b""
        if self.process.stderr and self.Stderr is not None:
            stderr_data = self.process.stderr.read()
            self.Stderr.write(stderr_data)
            
        returncode = self.process.wait()
        if returncode != 0:
            raise subprocess.CalledProcessError(returncode, self.args, stderr=stderr_data)

    def read(self, n=-1):
        if self.process is None or self.process.stdout is None:
            raise RuntimeError("stdout not available")
        return self.process.stdout.read(n)

    def close(self):
        if self.process and self.process.stdout:
            self.process.stdout.close()

class PathOutputLaunch:
    def start(self, cmd: Cmd):
        cmd.Start()
        return PathOutputJob()
    def close(self):
        pass

class PathOutputJob:
    def terminate(self):
        pass
    def close(self):
        pass

def prepare_path_output_command(cmd: Cmd) -> PathOutputLaunch:
    return PathOutputLaunch()

def stop_path_output_command(cmd: Cmd, stdout, job):
    if stdout is not None:
        try:
            stdout.close()
        except Exception:
            pass
    if cmd.process is not None:
        try:
            cmd.process.wait(timeout=cmd.WaitDelay)
        except subprocess.TimeoutExpired:
            try:
                cmd.process.kill()
            except Exception:
                pass
            try:
                cmd.process.wait(timeout=cmd.WaitDelay)
            except Exception:
                pass

def git_subprocess_environment(env: Dict[str, str], dir_path: str) -> Dict[str, str]:
    clean = env.copy()
    to_remove = [
        "GIT_DIR", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE",
        "GIT_OBJECT_DIRECTORY", "GIT_REPLACE_REF_BASE", "GIT_SHALLOW_FILE",
        "GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS"
    ]
    for key in list(clean.keys()):
        upper_key = key.upper()
        if (upper_key in to_remove or 
            upper_key.startswith("GIT_CONFIG_KEY_") or 
            upper_key.startswith("GIT_CONFIG_VALUE_") or 
            upper_key.startswith("GIT_TRACE") or 
            upper_key.startswith("GIT_REDIRECT_")):
            clean.pop(key, None)
            
    clean["GIT_CONFIG_NOSYSTEM"] = "1"
    clean["GIT_CONFIG_GLOBAL"] = os.devnull
    clean["GIT_CONFIG_SYSTEM"] = os.devnull
    clean["GIT_ATTR_NOSYSTEM"] = "1"
    clean["GIT_TERMINAL_PROMPT"] = "0"
    clean["GIT_ALLOW_PROTOCOL"] = ""
    clean["GIT_OPTIONAL_LOCKS"] = "0"
    
    # Pinned configs (7 of them):
    clean["GIT_CONFIG_COUNT"] = "7"
    clean["GIT_CONFIG_KEY_0"] = "core.fsmonitor"
    clean["GIT_CONFIG_VALUE_0"] = "false"
    clean["GIT_CONFIG_KEY_1"] = "log.showSignature"
    clean["GIT_CONFIG_VALUE_1"] = "false"
    clean["GIT_CONFIG_KEY_2"] = "core.excludesFile"
    clean["GIT_CONFIG_VALUE_2"] = ""
    clean["GIT_CONFIG_KEY_3"] = "core.attributesFile"
    clean["GIT_CONFIG_VALUE_3"] = ""
    clean["GIT_CONFIG_KEY_4"] = "submodule.recurse"
    clean["GIT_CONFIG_VALUE_4"] = "false"
    clean["GIT_CONFIG_KEY_5"] = "log.mailmap"
    clean["GIT_CONFIG_VALUE_5"] = "false"
    clean["GIT_CONFIG_KEY_6"] = "diff.orderFile"
    clean["GIT_CONFIG_VALUE_6"] = os.devnull
    
    return clean

def new_cmd(ctx, dir_path: str, name: str, *args: str) -> Cmd:
    env = os.environ.copy()
    if name == "git":
        env = git_subprocess_environment(env, dir_path)
    env["LC_ALL"] = "C"
    env["LANG"] = "C"
    return Cmd([name] + list(args), dir=dir_path, env=env)

def keep_directory_entry(field: str, seen: Set[str]) -> bool:
    if not field or not field.endswith("/"):
        return False
    if field in seen:
        return False
    seen.add(field)
    return True

def visit_bounded_nul_paths(cmd: Cmd, max_path_bytes: int, visit) -> None:
    stderr_buf = io.BytesIO()
    cmd.Stderr = stderr_buf
    
    launch = prepare_path_output_command(cmd)
    try:
        stdout = cmd.StdoutPipe()
        job = launch.start(cmd)
        try:
            record = bytearray()
            while True:
                b = stdout.read(1)
                if not b:
                    if len(record) > 0:
                        stop_path_output_command(cmd, stdout, job)
                        raise ValueError("git returned a non-NUL-terminated path")
                    break
                
                record.extend(b)
                if b == b'\x00':
                    path_str = record[:-1].decode('utf-8', errors='surrogateescape')
                    if not visit(path_str):
                        stop_path_output_command(cmd, stdout, job)
                        return None
                    record.clear()
                elif len(record) > max_path_bytes:
                    stop_path_output_command(cmd, stdout, job)
                    raise BufferError(f"git returned a path longer than {max_path_bytes} bytes")
            
            try:
                cmd.Wait()
            except subprocess.CalledProcessError as wait_err:
                msg = stderr_buf.getvalue().decode('utf-8', errors='replace').strip()
                if not msg:
                    msg = str(wait_err)
                raise Exception(msg) from wait_err
        finally:
            job.close()
    finally:
        launch.close()

def visit_bounded_worktree_path_output(cmd: Cmd, visit) -> None:
    budget = WorktreeListingBudget()
    return visit_bounded_worktree_path_output_with_budget(cmd, budget, visit)

def visit_bounded_worktree_path_output_with_budget(cmd: Cmd, budget: WorktreeListingBudget, visit) -> None:
    truncated = False
    
    def inner_visit(path: str) -> bool:
        nonlocal truncated
        if not budget.admit(path):
            truncated = True
            return False
        return visit(path)
        
    visit_bounded_nul_paths(cmd, NESTED_IGNORE_PATH_MAX_BYTES, inner_visit)
    if truncated:
        raise WorktreeListingTruncatedError()

def visit_worktree_directory_entry_output(cmd: Cmd, visit) -> None:
    seen = set()
    budget = IgnoredDirectoryListingBudget()
    truncated = False
    
    def inner_visit(entry: str) -> bool:
        nonlocal truncated
        if not budget.admit(entry):
            truncated = True
            return False
        if not keep_directory_entry(entry, seen):
            return True
        return visit(entry)
        
    visit_bounded_nul_paths(cmd, NESTED_IGNORE_PATH_MAX_BYTES, inner_visit)
    if truncated:
        raise IgnoredListingTruncatedError()

def TreeContainsPaths(ctx, repo: str, treeish: str, paths: List[str]) -> Set[str]:
    result = set()
    start = 0
    while start < len(paths):
        end = literal_pathspec_batch_end(paths, start)
        batch = tree_blob_members_batch(ctx, repo, treeish, paths[start:end])
        for path in batch:
            result.add(path)
        start = end
    return result

def tree_blob_members_batch(
    ctx,
    repo: str,
    treeish: str,
    paths: List[str],
) -> Set[str]:
    if not treeish or treeish.startswith("-") or "\x00" in treeish:
        raise ValueError(f"invalid treeish {treeish!r}")
        
    args = ["ls-tree", "-z", treeish, "--"]
    known = set()
    for path in paths:
        args.append(":(literal)" + path)
        known.add(path)
        
    cmd = new_cmd(ctx, repo, "git", *args)
    stdout = io.BytesIO()
    stderr = io.BytesIO()
    cmd.Stdout = stdout
    cmd.Stderr = stderr
    
    try:
        cmd.Run()
    except Exception as err:
        message = stderr.getvalue().decode('utf-8', errors='replace').strip()
        if not message:
            message = str(err)
        raise Exception(f"git ls-tree literal membership probe: {message}") from err
        
    data = stdout.getvalue()
    if len(data) > 0 and data[-1] != 0:
        raise ValueError("git ls-tree returned malformed membership output")
        
    result = set()
    records = data.split(b"\x00")
    for record in records:
        if len(record) == 0:
            continue
        tab = record.find(b"\t")
        if tab < 0:
            raise ValueError("git ls-tree returned a malformed membership record")
            
        header_bytes = record[:tab]
        header = header_bytes.decode('utf-8', errors='replace').split()
        if len(header) != 3:
            raise ValueError("git ls-tree returned a malformed membership header")
            
        member = record[tab+1:].decode('utf-8', errors='surrogateescape')
        if member not in known:
            raise ValueError(f"git ls-tree returned unexpected path {member!r}")
            
        if header[1] == "blob" or header[1] == "commit":
            result.add(member)
            
    return result

def FirstTreeNestedIgnorePaths(ctx, repo: str, treeish: str, limit: int) -> List[str]:
    if limit <= 0:
        return []
    validate_nested_ignore_limit(limit)
    args = ["ls-tree", "-r", "-z", "--name-only", treeish]
    return first_nested_ignore_paths(ctx, repo, args, limit, None)

def BoundedTreeNestedIgnorePaths(ctx, repo: str, treeish: str, limit: int) -> List[str]:
    if limit <= 0:
        return []
    validate_nested_ignore_limit(limit)
    args = ["ls-tree", "-r", "-z", "--name-only", treeish]
    return bounded_nested_ignore_paths(ctx, repo, args, limit, None, None)

def FirstWorktreeNestedIgnorePaths(
    ctx,
    repo: str,
    limit: int,
    include_ignored=None,
) -> List[str]:
    if limit <= 0:
        return []
    validate_nested_ignore_limit(limit)
    eligible_args = [
        "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--",
        ":(glob)**/.gitignore",
    ]
    paths = first_nested_ignore_paths(ctx, repo, eligible_args, limit, None)
    if len(paths) >= limit or include_ignored is None:
        return paths
        
    ignored_args = [
        "ls-files", "-z", "--others", "--ignored", "--exclude-standard",
        "--directory", "--no-empty-directory", "--",
        ":(glob)**/.gitignore",
    ]
    ignored = first_nested_ignore_paths(ctx, repo, ignored_args, limit - len(paths), include_ignored)
    return paths + ignored

def BoundedWorktreeNestedIgnorePaths(
    ctx,
    repo: str,
    limit: int,
    include_ignored=None,
) -> List[str]:
    if limit <= 0:
        return []
    validate_nested_ignore_limit(limit)
    eligible_args = [
        "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--",
        ":(glob)**/.gitignore",
    ]
    paths = bounded_nested_ignore_paths(ctx, repo, eligible_args, limit, None, None)
    if include_ignored is None:
        return paths
        
    ignored_args = [
        "ls-files", "-z", "--others", "--ignored", "--exclude-standard",
        "--directory", "--no-empty-directory", "--",
        ":(glob)**/.gitignore",
    ]
    return bounded_nested_ignore_paths(ctx, repo, ignored_args, limit, include_ignored, paths)

def VisitWorktreePaths(
    ctx,
    repo: str,
    ignored: bool,
    visit,
) -> None:
    if visit is None:
        raise ValueError("git worktree path visitor is nil")
    args = ["ls-files", "-z", "--others", "--exclude-standard"]
    if ignored:
        args.append("--ignored")
    else:
        args.append("--cached")
    return visit_bounded_worktree_path_output(new_cmd(ctx, repo, "git", *args), visit)

def VisitWorktreeDirectoryEntries(
    ctx,
    repo: str,
    ignored: bool,
    visit,
) -> None:
    if visit is None:
        raise ValueError("git worktree directory-entry visitor is nil")
    args = ["ls-files", "-z", "--others", "--exclude-standard", "--directory"]
    if ignored:
        args.append("--ignored")
    else:
        args.append("--cached")
    return visit_worktree_directory_entry_output(new_cmd(ctx, repo, "git", *args), visit)

def first_nested_ignore_paths(
    ctx,
    repo: str,
    args: List[str],
    limit: int,
    include=None,
) -> List[str]:
    if limit <= 0:
        return []
    validate_nested_ignore_limit(limit)
    paths = []
    retained_bytes = 0
    retained_limit_exceeded = False
    
    cmd = new_cmd(ctx, repo, "git", *args)
    
    def visit(path: str) -> bool:
        nonlocal retained_bytes, retained_limit_exceeded
        if "/" not in path or not path.endswith("/.gitignore"):
            return True
        if include is not None and not include(path):
            return True
        path_len = len(path.encode('utf-8'))
        if len(paths) >= NESTED_IGNORE_CANDIDATE_MAX_COUNT or \
           path_len > NESTED_IGNORE_CANDIDATE_MAX_ALL_BYTES - retained_bytes:
            retained_limit_exceeded = True
            return False
        paths.append(path)
        retained_bytes += path_len
        return len(paths) < limit

    visit_bounded_nul_paths(cmd, NESTED_IGNORE_PATH_MAX_BYTES, visit)
    
    if retained_limit_exceeded:
        raise ValueError(
            f"git nested-ignore candidates exceed {NESTED_IGNORE_CANDIDATE_MAX_COUNT} paths "
            f"or {NESTED_IGNORE_CANDIDATE_MAX_ALL_BYTES} aggregate bytes"
        )
    return paths

def bounded_nested_ignore_paths(
    ctx,
    repo: str,
    args: List[str],
    limit: int,
    include=None,
    paths: Optional[List[str]] = None,
) -> List[str]:
    if paths is None:
        paths = []
    paths = list(paths)
    
    if len(paths) > limit or len(paths) > NESTED_IGNORE_CANDIDATE_MAX_COUNT:
        raise ValueError(f"git nested-ignore candidates exceed {limit} paths")
        
    retained_bytes = 0
    seen = set()
    for retained in paths:
        seen.add(retained)
        retained_bytes += len(retained.encode('utf-8'))
        
    if retained_bytes > NESTED_IGNORE_CANDIDATE_MAX_ALL_BYTES:
        raise ValueError(
            f"git nested-ignore candidates exceed {NESTED_IGNORE_CANDIDATE_MAX_ALL_BYTES} aggregate bytes"
        )
        
    exceeded = False
    cmd = new_cmd(ctx, repo, "git", *args)
    
    def visit(candidate: str) -> bool:
        nonlocal retained_bytes, exceeded
        if "/" not in candidate or not candidate.endswith("/.gitignore"):
            return True
        if include is not None and not include(candidate):
            return True
        if candidate in seen:
            return True
            
        candidate_len = len(candidate.encode('utf-8'))
        if len(paths) >= limit or len(paths) >= NESTED_IGNORE_CANDIDATE_MAX_COUNT or \
           candidate_len > NESTED_IGNORE_CANDIDATE_MAX_ALL_BYTES - retained_bytes:
            exceeded = True
            return False
            
        seen.add(candidate)
        paths.append(candidate)
        retained_bytes += candidate_len
        return True

    visit_bounded_nul_paths(cmd, NESTED_IGNORE_PATH_MAX_BYTES, visit)
    
    if exceeded:
        raise ValueError(
            f"git nested-ignore candidates exceed {limit} paths "
            f"or {NESTED_IGNORE_CANDIDATE_MAX_ALL_BYTES} aggregate bytes"
        )
    return paths

def validate_nested_ignore_limit(limit: int) -> None:
    if limit > NESTED_IGNORE_CANDIDATE_MAX_COUNT:
        raise ValueError(
            f"git nested-ignore limit {limit} exceeds maximum {NESTED_IGNORE_CANDIDATE_MAX_COUNT}"
        )

def ClassifyWorktreePaths(
    ctx,
    repo: str,
    paths: List[str],
) -> Tuple[Set[str], Set[str]]:
    eligible = list_literal_worktree_paths(ctx, repo, paths, False)
    ignored = list_literal_worktree_paths(ctx, repo, paths, True)
    return eligible, ignored

def list_literal_worktree_paths(
    ctx,
    repo: str,
    paths: List[str],
    ignored: bool,
) -> Set[str]:
    result = set()
    start = 0
    while start < len(paths):
        end = literal_pathspec_batch_end(paths, start)
        batch = list_literal_worktree_path_batch(ctx, repo, paths[start:end], ignored)
        for path in batch:
            result.add(path)
        start = end
    return result

def literal_pathspec_batch_end(paths: List[str], start: int) -> int:
    end = start
    bytes_accum = 0
    while end < len(paths) and (end - start) < LITERAL_PATHSPEC_BATCH_COUNT:
        path_str = paths[end] if paths[end] is not None else ""
        pathspec_bytes = len(path_str.encode('utf-8')) + len(":(literal)")
        if end > start and (bytes_accum + pathspec_bytes) > LITERAL_PATHSPEC_BATCH_BYTES:
            break
        bytes_accum += pathspec_bytes
        end += 1
    return end

def run_bounded_path_output(
    cmd: Cmd,
    known: Set[str],
) -> Set[str]:
    if len(known) > LITERAL_PATHSPEC_BATCH_COUNT:
        raise ValueError(
            f"git literal path output input exceeds {LITERAL_PATHSPEC_BATCH_COUNT} paths"
        )
        
    expected_output_bytes = 0
    for path in known:
        path_len = len(path.encode('utf-8'))
        if path_len > LITERAL_PATH_OUTPUT_MAX_PATH_BYTES:
            raise ValueError(
                f"git literal path output input path exceeds {LITERAL_PATH_OUTPUT_MAX_PATH_BYTES} bytes"
            )
        record_bytes = path_len + 1
        if record_bytes > LITERAL_PATHSPEC_BATCH_BYTES - expected_output_bytes:
            raise ValueError(
                f"git literal path output input exceeds {LITERAL_PATHSPEC_BATCH_BYTES} aggregate bytes"
            )
        expected_output_bytes += record_bytes

    stderr_buf = io.BytesIO()
    cmd.Stderr = stderr_buf
    
    launch = prepare_path_output_command(cmd)
    try:
        stdout = cmd.StdoutPipe()
        job = launch.start(cmd)
        try:
            result = set()
            output_count = 0
            output_bytes = 0
            
            record = bytearray()
            while True:
                b = stdout.read(1)
                if not b:
                    if len(record) > 0:
                        stop_path_output_command(cmd, stdout, job)
                        raise ValueError("git returned a non-NUL-terminated path")
                    break
                
                record.extend(b)
                if b == b'\x00':
                    if len(record) == 1:
                        stop_path_output_command(cmd, stdout, job)
                        raise ValueError("git returned an empty path")
                        
                    path_str = record[:-1].decode('utf-8', errors='surrogateescape')
                    if path_str not in known:
                        stop_path_output_command(cmd, stdout, job)
                        raise ValueError(f"git returned unexpected path {path_str!r}")
                        
                    if path_str in result:
                        stop_path_output_command(cmd, stdout, job)
                        raise ValueError(f"git returned duplicate path {path_str!r}")
                        
                    output_count += 1
                    output_bytes += len(record)
                    if output_count > len(known) or output_count > LITERAL_PATHSPEC_BATCH_COUNT:
                        stop_path_output_command(cmd, stdout, job)
                        raise ValueError(
                            f"git returned more than {len(known)} literal paths"
                        )
                    if output_bytes > expected_output_bytes or output_bytes > LITERAL_PATHSPEC_BATCH_BYTES:
                        stop_path_output_command(cmd, stdout, job)
                        raise ValueError(
                            f"git returned more than {expected_output_bytes} aggregate path bytes"
                        )
                        
                    result.add(path_str)
                    record.clear()
                elif len(record) > LITERAL_PATH_OUTPUT_MAX_PATH_BYTES:
                    stop_path_output_command(cmd, stdout, job)
                    raise BufferError(
                        f"git returned a path longer than {LITERAL_PATH_OUTPUT_MAX_PATH_BYTES} bytes"
                    )
            
            try:
                cmd.Wait()
            except subprocess.CalledProcessError as wait_err:
                msg = stderr_buf.getvalue().decode('utf-8', errors='replace').strip()
                if not msg:
                    msg = str(wait_err)
                raise Exception(msg) from wait_err
                
            return result
        finally:
            job.close()
    finally:
        launch.close()

def list_literal_worktree_path_batch(
    ctx,
    repo: str,
    paths: List[str],
    ignored: bool,
) -> Set[str]:
    args = ["ls-files", "-z", "--others", "--exclude-standard"]
    if ignored:
        args.append("--ignored")
    else:
        args.append("--cached")
    args.append("--")
    
    known = set()
    for path in paths:
        args.append(":(literal)" + path)
        known.add(path)
        
    try:
        return run_bounded_path_output(new_cmd(ctx, repo, "git", *args), known)
    except Exception as err:
        raise Exception(f"git ls-files literal worktree probe: {err}") from err

def IndexHasFilesUnder(ctx, repo: str, rel: str) -> bool:
    dir_path = rel.rstrip("/")
    if not dir_path:
        return False
        
    args = [
        "ls-files", "-z", "--cached", "--error-unmatch", "--",
        ":(literal)" + dir_path + "/",
    ]
    cmd = new_cmd(ctx, repo, "git", *args)
    stderr_buf = io.BytesIO()
    cmd.Stderr = stderr_buf
    
    launch = prepare_path_output_command(cmd)
    try:
        stdout = cmd.StdoutPipe()
        job = launch.start(cmd)
        try:
            first = stdout.read(1)
            if len(first) > 0:
                stop_path_output_command(cmd, stdout, job)
                return True
                
            try:
                cmd.Wait()
            except subprocess.CalledProcessError as wait_err:
                if wait_err.returncode == 1:
                    return False
                msg = stderr_buf.getvalue().decode('utf-8', errors='replace').strip()
                if not msg:
                    msg = str(wait_err)
                raise Exception(f"git ls-files literal index probe: {msg}") from wait_err
                
            return False
        finally:
            job.close()
    finally:
        launch.close()
