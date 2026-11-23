#!/usr/bin/env python3
"""Open a review-only PR for the latest upstream stable release. Never updates main."""
import base64
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

UPSTREAM = "Tencent/WeKnora"
TAG = re.compile(r"^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")


class API:
    def __init__(self, token):
        self.token = token

    def __call__(self, path, data=None, method=None):
        request = urllib.request.Request(
            "https://api.github.com" + path,
            data=json.dumps(data).encode() if data is not None else None,
            method=method,
            headers={"Authorization": "Bearer " + self.token,
                     "Accept": "application/vnd.github+json",
                     "X-GitHub-Api-Version": "2022-11-28"},
        )
        with urllib.request.urlopen(request, timeout=45) as response:
            body = response.read()
            return json.loads(body) if body else {}


def validate_release(release):
    tag = release.get("tag_name", "")
    if release.get("draft") or release.get("prerelease") or not TAG.fullmatch(tag):
        raise ValueError("Upstream latest release is not a supported stable version")
    return tag


def release_commit(api, tag):
    obj = api(f"/repos/{UPSTREAM}/git/ref/tags/{tag}")["object"]
    for _ in range(5):
        if obj["type"] == "commit":
            if not re.fullmatch(r"[0-9a-f]{40}", obj["sha"]):
                raise ValueError("Invalid release commit")
            return obj["sha"]
        if obj["type"] != "tag":
            break
        obj = api(f"/repos/{UPSTREAM}/git/tags/{obj['sha']}")["object"]
    raise ValueError("Could not resolve release tag to a commit")


def emit(**outputs):
    if os.environ.get("GITHUB_OUTPUT"):
        with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as stream:
            for key, value in outputs.items():
                if "\n" in str(value):
                    raise ValueError("Invalid multiline output")
                stream.write(f"{key}={value}\n")


def synchronize(api, repo):
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo) or repo == UPSTREAM:
        raise ValueError("Expected a fork repository")
    root = "/repos/" + repo
    metadata = api(root)
    if metadata.get("default_branch") != "main":
        raise ValueError("This automation expects main; update configuration explicitly")
    release = api(f"/repos/{UPSTREAM}/releases/latest")
    tag = validate_release(release)
    upstream_sha = release_commit(api, tag)
    base_sha = api(root + "/git/ref/heads/main")["object"]["sha"]
    comparison = api(root + f"/compare/{upstream_sha}...{base_sha}")
    if comparison["status"] in ("ahead", "identical"):
        print(f"{tag} is already included in main; no upgrade PR needed.")
        emit(candidate_sha="", pr_number="")
        return 0

    branch = f"upstream-release/{tag}-{upstream_sha[:8]}"
    head = repo.split("/")[0] + ":" + branch
    pulls = api(root + "/pulls?state=all&head=" + urllib.parse.quote(head, safe="") + "&base=main")
    if pulls:
        print("An upgrade PR already exists; it will not be overwritten or reopened: " + pulls[0]["html_url"])
        emit(candidate_sha="", pr_number="")
        return 0

    try:
        api(root + "/git/ref/heads/" + branch)
    except urllib.error.HTTPError as error:
        if error.code != 404:
            raise
        api(root + "/git/refs", {"ref": "refs/heads/" + branch, "sha": base_sha})

    blocked = ""
    try:
        # Server-side merge of existing commits in the fork network. Unlike a
        # force-sync, this retains the fork's custom commits. Only this new
        # candidate branch is updated; main is never a mutation target.
        merged = api(root + "/merges", {
            "base": branch, "head": upstream_sha,
            "commit_message": f"Merge upstream stable {tag} into upgrade candidate",
        })
        candidate_sha = merged.get("sha") or api(root + "/git/ref/heads/" + branch)["object"]["sha"]
    except urllib.error.HTTPError as error:
        if error.code not in (403, 409):
            raise
        blocked = "merge conflict" if error.code == 409 else "repository permission restriction"
        marker = (f"# Upstream upgrade blocked: {tag}\n\n"
                  f"Reason: {blocked}. Upstream code has NOT been merged.\n\n"
                  f"- Upstream: {UPSTREAM}@{upstream_sha}\n"
                  f"- Fork base: {base_sha}\n\n"
                  "Resolve the upgrade manually on this branch and remove this marker. "
                  "Then use Fork CI (workflow_dispatch) to validate the branch. "
                  "Do not discard custom commits or force-sync main.\n")
        marker_path = root + "/contents/.github/upstream-conflicts/" + tag + ".md"
        payload = {
            "branch": branch, "message": f"Record blocked upstream upgrade {tag}",
            "content": base64.b64encode(marker.encode()).decode(),
        }
        try:
            old = api(marker_path + "?ref=" + urllib.parse.quote(branch, safe=""))
            payload["sha"] = old["sha"]
        except urllib.error.HTTPError as lookup_error:
            if lookup_error.code != 404:
                raise
        api(marker_path, payload, "PUT")
        candidate_sha = ""

    title = ("[Blocked] " if blocked else "") + f"Upgrade upstream stable release {tag}"
    body = (f"Upstream release: https://github.com/{UPSTREAM}/releases/tag/{tag}\n\n"
            f"Upstream commit: `{upstream_sha}`\nFork base: `{base_sha}`\n\n"
            "This PR preserves the fork's commits. It is never automatically merged or deployed.\n\n")
    if blocked:
        body += f"**Stopped: {blocked}.** This draft contains a diagnostic marker, not an applied upgrade. Resolve it manually before testing."
    else:
        body += ("Fork regression tests and image builds run as jobs of the stable-release sync workflow. "
                 "A failed check stops image building. Review the linked workflow run before merging. "
                 "PR builds are not published to GHCR; a manual merge to main triggers tested SHA-tagged images.")
    if os.environ.get("GITHUB_RUN_ID"):
        body += f"\n\nValidation run: https://github.com/{repo}/actions/runs/{os.environ['GITHUB_RUN_ID']}"
    pr = api(root + "/pulls", {"title": title, "head": branch, "base": "main", "body": body, "draft": bool(blocked)})
    print(pr["html_url"])
    emit(candidate_sha=candidate_sha, pr_number=pr["number"])
    return 1 if blocked else 0


if __name__ == "__main__":
    try:
        sys.exit(synchronize(API(os.environ["GH_TOKEN"]), os.environ["GITHUB_REPOSITORY"]))
    except Exception as error:
        # Do not echo response bodies or authentication headers.
        print(f"Stable release sync stopped: {type(error).__name__}: {error}", file=sys.stderr)
        sys.exit(1)
