# Fork 的稳定版升级与镜像构建

适用仓库：nanwanwang/WeKnora。只操作 GitHub 仓库和 GHCR，不连接任何服务器，不自动合并、不部署。

## 自动运行规则

- 每天北京时间 09:23 检查 Tencent/WeKnora 最新非草稿、非预发布的稳定 release（vX.Y.Z）。GitHub 定时任务可能延迟。
- 如果该 release 已包含在 main 中，不建重复 PR。不会追踪官方 main 的日常开发提交。
- 从本仓库 main 建候选分支，将官方稳定版提交合并进去，保留本仓库修改。绝不强制同步或覆盖 main。
- 新候选分支创建升级 PR，并由同步工作流直接运行回归和试构建，不依赖机器人 PR 事件触发。
- 合并冲突或权限限制时创建带原因记录的草稿 PR，然后失败停止。手工解决冲突并删除 .github/upstream-conflicts 下对应文件后，再运行 Fork CI 检查该分支。
- 测试失败时不运行镜像构建；构建失败时状态失败。不会自动重试、关闭、合并 PR，也不会重开已被人工关闭的同一版本 PR。
- 手动合入 main 后，重新验证并发布 SHA 固定标签镜像。没有 latest 标签，没有服务器部署步骤。

## 自动检查覆盖

检查工单号提取、边界、大小写、去重，以及 Qdrant payload 的精确编号与序列化回归；关键测试缺失也会失败。
同时执行 Go 包测试、go vet、服务端构建、同步脚本单元测试；使用项目原始 Dockerfile 构建 app（含 AnyDoc）、ui、docreader 的 linux/amd64 镜像。

这些检查不能替代真实知识库的端到端验证。正式更新前，仍需在测试环境验证 PA-24110315 的检索、回答与原文引用，以及 PDF/Office/扫描件解析。升级不重新解析已有知识库；数据库迁移和存储兼容性需按官方 release notes 审查。

## 镜像

通过验证的 main 提交发布到：
- ghcr.io/nanwanwang/weknora-app:sha-完整提交号
- ghcr.io/nanwanwang/weknora-ui:sha-完整提交号
- ghcr.io/nanwanwang/weknora-docreader:sha-完整提交号

PR 和手动 Fork CI 只试构建，不发布。构建缓存加速后续运行；首次构建较慢。组件独立发布，只有整个 Fork CI 成功才视为可用版本，不能把单个镜像出现当作整套构建通过。

## 权限与维护

使用 GitHub 内置 GITHUB_TOKEN，不保存 PAT、服务器密码或部署密钥。仓库 Actions 默认权限维持只读；同步 job 单独获得 contents/pull-requests 写权限，镜像 job 单独获得 packages 写权限，结果报告 job 单独获得 statuses 写权限。

仓库 Settings → Actions → General 中需要允许 GitHub Actions 创建 Pull Request。这个选项同时包含批准权限，但本流程不批准 PR。
上游原 Docker Hub 发布工作流限定只在 Tencent/WeKnora 运行，避免 Fork 误发官方镜像。
定时工作流必须存在于默认 main 分支。GitHub 公共仓库连续 60 天无活动可能自动停用定时工作流，可在 Actions 中重新启用。
如果未来 GitHub 权限规则变化导致服务端合并被拒绝，会留下受阻 PR，不绕过检查，也不把官方工作流文件从升级中偷偷剔除。

人工修复后：Actions → Fork CI → Run workflow，ref 填候选分支或完整提交号；确认全绿再人工合并。不要使用丢弃 Fork 修改的强制同步方式。此流程没有启用自动合并，用户仍可自行手动合并失败 PR，因此合并前必须检查运行结果。
