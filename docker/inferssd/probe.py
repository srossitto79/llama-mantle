"""Check Docker's io_uring and cgroup prerequisites without loading a model (x86_64)."""

import ctypes
import json
import os
from pathlib import Path
import platform
import resource
import sys

if platform.machine() not in ("x86_64", "aarch64"):
    sys.exit("io_uring probe supports x86_64/aarch64 Linux only")
libc = ctypes.CDLL(None, use_errno=True)
parameters = ctypes.create_string_buffer(256)
descriptor = libc.syscall(425, 2, parameters)  # io_uring_setup on x86_64/aarch64
error = ctypes.get_errno() if descriptor < 0 else 0
if descriptor >= 0:
    os.close(descriptor)
root = Path("/sys/fs/cgroup")
report = {
    "io_uring_errno": error,
    "memory_max": (root / "memory.max").read_text().strip(),
    "swap_max": (root / "memory.swap.max").read_text().strip(),
    "membership": Path("/proc/self/cgroup").read_text().strip(),
    "memlock": resource.getrlimit(resource.RLIMIT_MEMLOCK)[0],
}
print(json.dumps(report, indent=2))
sys.exit(0 if error == 0 and report["memory_max"] != "max" and report["swap_max"] == "0"
         and report["memlock"] == resource.RLIM_INFINITY else 1)
