#!/usr/bin/env python3
"""Backfill exact identifier payloads without reparsing or replacing vectors."""
import argparse
import hashlib
import json
import os
import re
import time
import urllib.request
from pathlib import Path

PATTERN = re.compile(r"(?<![A-Za-z0-9_-])[A-Za-z]{2,8}-[0-9]{6,20}(?![A-Za-z0-9_-])")


def identifiers(content):
    return list(dict.fromkeys(match.upper() for match in PATTERN.findall(content)))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default=os.environ.get("QDRANT_URL", "http://localhost:6333"))
    parser.add_argument("--collection", required=True)
    parser.add_argument("--apply", action="store_true", help="Write payloads and create the keyword index; otherwise dry-run")
    parser.add_argument("--backup", type=Path, help="New JSONL backup path, required with --apply")
    args = parser.parse_args()
    if args.apply and not args.backup:
        parser.error("--apply requires --backup (an existing file is never overwritten)")

    headers = {"Content-Type": "application/json"}
    if os.environ.get("QDRANT_API_KEY"):
        headers["api-key"] = os.environ["QDRANT_API_KEY"]

    def request(path, body=None, method=None):
        req = urllib.request.Request(
            args.url.rstrip("/") + path,
            data=json.dumps(body).encode() if body is not None else None,
            headers=headers, method=method,
        )
        with urllib.request.urlopen(req, timeout=30) as response:
            return json.load(response)["result"]

    from urllib.parse import quote
    collection = "/collections/" + quote(args.collection, safe="")
    before = request(collection)["points_count"]
    # A backup contains point IDs and previous values, never document content,
    # vectors or API keys. Protect it as operational data.
    backup = None
    if args.apply:
        fd = os.open(args.backup, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        backup = os.fdopen(fd, "w", encoding="utf-8")
    scanned = changed = 0
    offset = None
    try:
        while True:
            body = {"limit": 256, "with_payload": ["content", "exact_identifiers"], "with_vector": False}
            if offset is not None:
                body["offset"] = offset
            page = request(collection + "/points/scroll", body)
            operations = []
            for point in page["points"]:
                scanned += 1
                payload = point.get("payload") or {}
                content = payload.get("content", "")
                found = identifiers(content)
                if payload.get("exact_identifiers", []) == found:
                    continue
                changed += 1
                if args.apply:
                    backup.write(json.dumps({
                        "id": point["id"],
                        "had_field": "exact_identifiers" in payload,
                        "old_value": payload.get("exact_identifiers"),
                        "content_sha256": hashlib.sha256(content.encode()).hexdigest(),
                    }) + "\n")
                    operations.append({"set_payload": {
                        "payload": {"exact_identifiers": found}, "points": [point["id"]],
                    }})
            if operations:
                backup.flush()
                os.fsync(backup.fileno())
                request(collection + "/points/batch?wait=true", {"operations": operations})
            offset = page.get("next_page_offset")
            if offset is None:
                break
            time.sleep(0.03)
        if args.apply:
            request(collection + "/index?wait=true",
                    {"field_name": "exact_identifiers", "field_schema": "keyword"}, "PUT")
    finally:
        if backup:
            backup.close()
    print(json.dumps({
        "mode": "apply" if args.apply else "dry-run",
        "scanned": scanned, "changed": changed,
        "points_before": before, "points_after": request(collection)["points_count"],
    }))


if __name__ == "__main__":
    main()
