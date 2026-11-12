# 精确工单编号检索（Fork 定制）

本修改给 Qdrant 增加完整编号匹配，解决语义相似编号或公共字母前缀挤掉目标记录的问题。
它不修改文档解析器，不重建正文分块，也不重新计算向量。

## 行为及边界

- 识别格式：2–8 个 ASCII 字母、一个半角连字符、6–20 位数字。
  例如合成编号 `WO-12345678`，大小写不敏感。
- 以完整 ASCII 编号边界匹配，不把 `WO-123456780`、`XWO-12345678` 或
  `WO-12345678-REV` 当作 `WO-12345678`。
- Qdrant 新增 `exact_identifiers` keyword 数组；新写入的索引自动提取该字段。
- 包含这类编号的问题，关键词召回使用完整编号；混合召回融合后、
  截取候选之前，只保留包含请求编号的内容。
- 多个编号采用“匹配其中任一个”的语义，仍受原有候选数量限制。
- 无此类编号的问题沿用原检索流程。既有知识库、文档和启用状态过滤仍然生效。

这是严格筛选：编号拼错时不会用相近编号兜底；未写该编号的背景资料也可能被排除。
纯数字编码、其他编码格式和字段范围查询不在本修改范围内。
当前规则不可通过 UI 配置，也没有单独的功能开关。

服务层筛选位于公共检索链路，因此所有后端的编号查询都受其影响；
本次新增的专用精确索引路径和验证范围是 **Qdrant**。其他后端需单独验证，
不要把本修改作为已验证的通用全文检索方案。

## 已有数据迁移

先备份部署配置及数据，并安排没有索引写入的维护窗口。
使用 Python 3 标准库脚本，每个实际使用的 Qdrant collection 都需要处理：

```bash
export QDRANT_URL=http://localhost:6333
# 如启用鉴权，在环境中提供 QDRANT_API_KEY，不要写入仓库。
python3 scripts/backfill-exact-identifiers.py --collection YOUR_COLLECTION
python3 scripts/backfill-exact-identifiers.py --collection YOUR_COLLECTION \
  --apply --backup /secure/path/exact-identifiers-before.jsonl
```

默认 dry-run。应用模式只执行 SetPayload 并创建一个 keyword 索引，
不会删除 point、覆盖其他 payload、读取或重写向量。
备份文件使用独占创建，已有文件不会覆盖；脚本可再次执行以补齐漏项。
运行失败时保留备份和错误，查清原因后再重试。
先完成全部 collection 的回填，再部署补丁；仅回填部分数据可能造成编号查询漏召回。
迁移期间如仍有旧版应用写入索引，切换后需要再次补齐。

## 构建与升级

优先使用仓库原有 `docker/Dockerfile.app` 构建，保留 `WITH_ANYDOC=1`。
该 Dockerfile 会同时打包 GoJieba 字典；不要仅替换二进制而遗漏其运行时词典。

如采用定制构建路径，应设置 `JIEBA_DICT_DIR` 指向镜像内可读的实际词典目录。
不要硬编码某台测试服务器的路径，也不要为词典权限将应用改为 root 运行。

升级时将官方稳定版本合入定制分支，检查以下文件的冲突：
- `internal/types/exact_identifier.go`
- `internal/application/repository/retriever/qdrant/repository.go`
- `internal/application/service/knowledgebase_search.go`

官方实现等效能力后，应评估删除此定制，避免长期维护两套行为。
本次提交保留 Fork 当前的上游改动，不覆盖其新增的图片查询等功能。

## 验证

```bash
go test ./internal/types -run 'TestExactIdentifiers|TestContainsExactIdentifier'
go test ./internal/application/repository/retriever/qdrant -run TestCreatePayloadExactIdentifiers
```

还需在测试环境验证：
- 原文存在的编号、相近编号、不存在的编号、大小写和中文提问；
- 普通中文检索，以及限制到不含目标记录的文档时返回空结果；
- 问答回答、引用原文可读取，分块数量和原解析时间不变；
- 新上传/编辑内容能写入编号字段；
- 不同后端或格式的兼容性（若使用）。

检索命中不等于模型必然逐条完整总结。统计全部记录应使用结构化查询，
不能把生成回答中的条数当作数据库精确计数。

## 回退

恢复原应用镜像和部署配置即可停止使用新增查询逻辑。
新增 payload 字段及 keyword 索引可暂时保留，原版应用不会使用它们。
如需移除，依据 JSONL 备份逐 point 恢复旧字段值；原先没有该字段的记录只删除该字段。
不要删除 collection 或重新解析文档作为回退步骤。
