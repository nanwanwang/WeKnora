# DeepSeek and Tencent Cloud search (v0.9.0 backport)

This branch is based on f17ee8a8da901bd658a4cb25f40b5fd41adbfdd7 to match the existing deployment. It ports standalone DeepSeek search without bringing in newer fetch interfaces or unrelated migrations.

In Settings > Web Search Configuration, add **DeepSeek Search** or **Tencent Cloud Search (Sogou / Yuanbao)**. Enter the provider service API key, test the connection, then set as default or choose it for an agent. Enable web search in the agent and conversation.

- DeepSeek: https://api-docs.deepseek.com/api/search/ ; POST /v1/tools/search. Queries are limited to 128 Unicode characters, result counts to 1–50. Long summaries are preferred for model context. No chat-model change is needed.
- Tencent: https://cloud.tencent.com/document/product/1806/130615 ; POST https://api.wsa.cloud.tencent.com/SearchPro. Use a **WSA service API Key**, not CAM SecretId/SecretKey and not a Zhipu key. Public webpage mode is used. Requested counts round up to the next supported multiple of ten, then results are trimmed locally. Dynamic summaries are not treated as full page content.
- Both reuse encrypted credential storage and optional HTTP proxy settings. Endpoints are fixed. No schema migration is required.
- Live provider acceptance requires valid keys and quota. Offline tests use local HTTP fixtures and do not call paid services.

Validation: `go test ./internal/infrastructure/web_search ./internal/application/service -run 'Standalone|Zhipu|Bocha|Registry'` and compile the application before deploying. Deployment must be limited to 148; 152 is explicitly excluded.

## Build and test deployment

The current v0.9.0 runtime was built with `-tags anydoc`. Preserve that flag and the parser library when rebuilding. The compatibility Dockerfile includes SQLite development headers and removes unused LLVM bitcode metadata from the Rust archive to satisfy the Go linker without disabling its checks.

```sh
docker build -f docker/Dockerfile.search-v090 --build-arg COMMIT_ID=$(git rev-parse HEAD) -t weknora-search:test .
```

The 148 test deployment on 2026-11-16 uses `weknora-mom-pilot-app:search-20261116`, code commit `921c916c38d616d3b1daa00d0ac6244960a19a41`, and a separate `compose.search.yml` image override beside the existing compose files. Include that override for subsequent test restarts:

```sh
docker compose --profile '*' -p weknora-mom-pilot -f docker-compose.yml -f compose.pilot.yml -f compose.search.yml up -d --no-deps --pull never app
```

Rollback: change only the app image in that override to the retained `weknora-mom-pilot-app:before-search-20261116` tag, then run the same command. No database migration is introduced by this branch.

Verified: search adapter and service tests, complete application build with anydoc, healthy test container, both provider types returned by the authenticated API, temporary configuration creation/readback, hidden credentials, and removal of temporary test records. Live paid-provider calls remain pending: the user will enter real keys in the settings page. No changes were made to 152.
