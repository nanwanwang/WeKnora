import importlib.util
import pathlib
import unittest
import urllib.error

spec = importlib.util.spec_from_file_location("sync_stable", pathlib.Path(__file__).parents[1] / "sync_stable.py")
sync = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sync)
BASE = "b" * 40
UPSTREAM = "a" * 40
MERGED = "c" * 40


class FakeAPI:
    def __init__(self, status="diverged", error=None, existing=False):
        self.status, self.error, self.existing = status, error, existing
        self.writes = []

    def __call__(self, path, data=None, method=None):
        if data is not None:
            self.writes.append((path, data, method))
            if path.endswith("/merges"):
                if self.error:
                    raise urllib.error.HTTPError(path, self.error, "blocked", {}, None)
                return {"sha": MERGED}
            if path.endswith("/pulls"):
                return {"html_url": "https://github.com/test/fork/pull/1", "number": 1}
            return {}
        if path == "/repos/test/fork":
            return {"default_branch": "main"}
        if path.endswith("/releases/latest"):
            return {"tag_name": "v1.2.3", "draft": False, "prerelease": False}
        if "/git/ref/tags/" in path:
            return {"object": {"type": "commit", "sha": UPSTREAM}}
        if path.endswith("/git/ref/heads/main"):
            return {"object": {"sha": BASE}}
        if "/compare/" in path:
            return {"status": self.status}
        if "/pulls?" in path:
            return [{"html_url": "existing"}] if self.existing else []
        raise urllib.error.HTTPError(path, 404, "not found", {}, None)


class StableSyncTests(unittest.TestCase):
    def test_reject_unstable_tags(self):
        for tag in ("v1.2.3-rc1", "nightly", "../main", "v1.2"):
            with self.assertRaises(ValueError):
                sync.validate_release({"tag_name": tag})
        for field in ("draft", "prerelease"):
            with self.assertRaises(ValueError):
                sync.validate_release({"tag_name": "v1.2.3", field: True})

    def test_already_included_does_not_mutate(self):
        for status in ("ahead", "identical"):
            api = FakeAPI(status=status)
            self.assertEqual(sync.synchronize(api, "test/fork"), 0)
            self.assertEqual(api.writes, [])

    def test_existing_open_or_closed_pr_is_not_overwritten(self):
        api = FakeAPI(existing=True)
        self.assertEqual(sync.synchronize(api, "test/fork"), 0)
        self.assertEqual(api.writes, [])

    def test_upgrade_only_changes_candidate_branch(self):
        api = FakeAPI()
        self.assertEqual(sync.synchronize(api, "test/fork"), 0)
        merge = next(data for path, data, _ in api.writes if path.endswith("/merges"))
        self.assertTrue(merge["base"].startswith("upstream-release/"))
        self.assertEqual(merge["head"], UPSTREAM)
        self.assertFalse(api.writes[-1][1]["draft"])
        self.assertFalse(any("/pulls/1/merge" in path for path, _, _ in api.writes))

    def test_conflict_or_permission_error_stops_with_draft(self):
        for status in (403, 409):
            api = FakeAPI(error=status)
            self.assertEqual(sync.synchronize(api, "test/fork"), 1)
            self.assertTrue(api.writes[-1][1]["draft"])
            self.assertIn("[Blocked]", api.writes[-1][1]["title"])
            self.assertTrue(any("/upstream-conflicts/" in path for path, _, _ in api.writes))


if __name__ == "__main__":
    unittest.main()
