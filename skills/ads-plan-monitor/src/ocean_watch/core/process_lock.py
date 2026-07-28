#!/usr/bin/env python3
import json
import os
import secrets
import time
from pathlib import Path


class ProcessLock:
    def __init__(self, path, timeout=60):
        self.path = Path(path)
        self.timeout = timeout
        self.handle = None
        self.nonce = None

    def _try_lock(self):
        if os.name == "nt":
            import msvcrt

            self.handle.seek(0)
            try:
                msvcrt.locking(self.handle.fileno(), msvcrt.LK_NBLCK, 1)
                return True
            except OSError:
                return False

        import fcntl

        try:
            fcntl.flock(self.handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            return True
        except BlockingIOError:
            return False

    def _unlock(self):
        if os.name == "nt":
            import msvcrt

            self.handle.seek(0)
            msvcrt.locking(self.handle.fileno(), msvcrt.LK_UNLCK, 1)
            return

        import fcntl

        fcntl.flock(self.handle.fileno(), fcntl.LOCK_UN)

    def __enter__(self):
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.path.open("a+b") as initializer:
            initializer.seek(0, os.SEEK_END)
            if initializer.tell() == 0:
                initializer.write(b" ")
                initializer.flush()
                os.fsync(initializer.fileno())
        self.handle = self.path.open("r+b")
        deadline = time.monotonic() + self.timeout
        while not self._try_lock():
            if time.monotonic() >= deadline:
                self.handle.close()
                self.handle = None
                raise TimeoutError(f"timed out waiting for process lock: {self.path}")
            time.sleep(0.1)
        self.nonce = secrets.token_hex(8)
        metadata = json.dumps({"pid": os.getpid(), "nonce": self.nonce}).encode("utf-8")
        self.handle.seek(0, os.SEEK_END)
        record_size = max(self.handle.tell(), len(metadata))
        self.handle.seek(0)
        self.handle.write(metadata.ljust(record_size, b" "))
        self.handle.flush()
        os.fsync(self.handle.fileno())
        return self

    def __exit__(self, exc_type, exc, tb):
        if self.handle is None:
            return
        try:
            self._unlock()
        finally:
            self.handle.close()
            self.handle = None
