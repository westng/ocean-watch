#!/usr/bin/env python3
import hashlib
import json
import os
import tempfile
from contextlib import contextmanager
from pathlib import Path

from ocean_watch.core.errors import ConfigurationConflictError
from ocean_watch.core.process_lock import ProcessLock


def load_json(path):
    path = Path(path)
    return json.loads(path.read_text(encoding="utf-8"))


def atomic_write_text(path, payload, backup=True):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    try:
        os.chmod(path.parent, 0o700)
    except OSError:
        pass
    descriptor, temporary_name = tempfile.mkstemp(
        prefix=f".{path.name}.",
        suffix=".tmp",
        dir=path.parent,
    )
    temporary_path = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
        if backup and path.exists():
            backup_path = path.with_suffix(path.suffix + ".bak")
            atomic_write_text(
                backup_path,
                path.read_text(encoding="utf-8"),
                backup=False,
            )
        os.replace(temporary_path, path)
        try:
            os.chmod(path, 0o600)
        except OSError:
            pass
        if path.read_text(encoding="utf-8") != payload:
            raise OSError(f"atomic file write verification failed: {path}")
        try:
            directory_fd = os.open(path.parent, os.O_RDONLY)
        except OSError:
            directory_fd = None
        if directory_fd is not None:
            try:
                os.fsync(directory_fd)
            except OSError:
                pass
            finally:
                os.close(directory_fd)
    except Exception:
        temporary_path.unlink(missing_ok=True)
        raise


def atomic_write_json(path, data, backup=True):
    payload = json.dumps(data, ensure_ascii=False, indent=2) + "\n"
    atomic_write_text(path, payload, backup=backup)


def lock_path(path):
    path = Path(path)
    return path.with_suffix(path.suffix + ".lock")


@contextmanager
def json_file_lock(path, *, lock_timeout=60):
    path = Path(path)
    with ProcessLock(lock_path(path), timeout=lock_timeout):
        yield path


def update_json(path, updater, *, backup=True, lock_timeout=60):
    """Run a read-modify-write callback while holding a process lock."""
    path = Path(path)
    with json_file_lock(path, lock_timeout=lock_timeout):
        updated, result = updater(load_json(path))
        atomic_write_json(path, updated, backup=backup)
        return result


def initialize_json(path, factory, *, overwrite=False, backup=True, lock_timeout=60):
    """Create a JSON file exactly once, or replace it while holding its lock."""
    path = Path(path)
    with json_file_lock(path, lock_timeout=lock_timeout):
        existed = path.exists()
        if existed and not overwrite:
            return False
        atomic_write_json(
            path,
            factory(),
            backup=backup and existed,
        )
        return True


def json_revision(data):
    payload = json.dumps(data, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()


def compare_and_swap_json(path, expected_revision, updated, *, backup=True, lock_timeout=60):
    path = Path(path)
    with json_file_lock(path, lock_timeout=lock_timeout):
        current = load_json(path)
        if json_revision(current) != expected_revision:
            raise ConfigurationConflictError(
                "configuration changed while this operation was running; reload and retry"
            )
        atomic_write_json(path, updated, backup=backup)
