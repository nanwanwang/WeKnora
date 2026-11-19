# Document-specific answer rules

Knowledge QA can append operator-managed instructions to a single expert's system prompt when the retrieved evidence and file version match. Leave `WEKNORA_DOCUMENT_ANSWER_RULES_FILE` unset to use the expert's regular prompt on its own.

Mount a trusted JSON file in the application container and set `WEKNORA_DOCUMENT_ANSWER_RULES_FILE` to its path inside the container. The application reads it once per process. Restart the application to load changes to the file or environment variable.

```json
{
  "agent_id": "expert-id",
  "rules": [
    {
      "knowledge_id": "document-id",
      "file_hash": "value-from-the-document-api",
      "evidence_terms": ["workflow", "approval"],
      "instruction": "Preserve the original numbering when citing the workflow."
    }
  ]
}
```

- Use the knowledge record's `id` and `file_hash` returned by `GET /api/v1/knowledge/{id}`. Configure at most one rule per document in the file.
- All evidence terms must appear in the selected document's retrieved passages. Matching is case-insensitive. A document with the same title and a different ID cannot activate the rule.
- Replacing the file invalidates the rule when its hash changes. Review the rule against the new document before updating the saved hash.
- If the rule file or the authorized version lookup fails, generation continues with the expert's regular prompt. Updating the prompt rule file requires no changes to document indexes.
