#!/usr/bin/env python3
"""Attach isolated validation results; never merge a PR."""
import os
import re
from sync_stable import API

sha = os.environ["CANDIDATE_SHA"]
if not re.fullmatch(r"[0-9a-f]{40}", sha):
    raise ValueError("Expected an exact candidate commit")
repo = os.environ["GITHUB_REPOSITORY"]
success = all(os.environ[key] == "success" for key in ("VERIFY_RESULT", "IMAGES_RESULT"))
API(os.environ["GH_TOKEN"])(f"/repos/{repo}/statuses/{sha}", {
    "state": "success" if success else "failure",
    "context": "fork-upgrade-validation",
    "description": "Regression tests and images passed" if success else "Stopped: regression tests or images failed",
    "target_url": f"https://github.com/{repo}/actions/runs/{os.environ['GITHUB_RUN_ID']}",
})
