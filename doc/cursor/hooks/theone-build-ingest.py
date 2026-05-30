#!/usr/bin/env python3
"""文档镜像：委托 drivers/shared/theone-build-ingest.py。"""
import os
import subprocess
import sys

_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "../../.."))
_SHARED = os.path.join(_ROOT, "drivers", "shared", "theone-build-ingest.py")

if __name__ == "__main__":
    env = os.environ.copy()
    env.setdefault("THEONE_AGENT_TYPE", "cursor")
    sys.exit(subprocess.call([sys.executable, _SHARED, *sys.argv[1:]], env=env))
