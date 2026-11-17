package modelcontext

// CitationTemplate issues a short citation only for already verified current
// evidence. It does not register sources or authorize retrieval. Numbers use a
// single request-local namespace across knowledge and web sources; they are
// never inferred from chunk numbers or restored from historical answers.
func (r *Registry) CitationTemplate(handle string) string {
	if r == nil || r.sources == nil {
		return ""
	}
	handle = canonicalSourceHandle(handle)
	if handle == "" || r.sources.citationTag(handle) == "" {
		return ""
	}
	alias := r.sources.citationAliases.register(handle, handle, struct{}{}, nil)
	return `<ref id="` + alias + `"/>`
}
