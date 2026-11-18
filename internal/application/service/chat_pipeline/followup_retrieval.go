package chatpipeline

import (
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/searchutil"
)

var (
	dimensionExpression = regexp.MustCompile("(?i)[0-9]+(?:\\.[0-9]+)?(?:[ \\t]*[×x*][ \\t]*[0-9]+(?:\\.[0-9]+)?)+")
	explicitFilename    = regexp.MustCompile("(?i)\\.(?:pdf|docx?|xlsx?|pptx?|txt|md)\\b")
	dimensionSeparator  = strings.NewReplacer("x", "×", "X", "×", "*", "×")
)

// qualifyFollowup carries explicit context from the previous user question into
// retrieval. The new turn supplies its own conditions (e.g. current type or
// temperature). Never use a previous answer or attachment as query context.
func qualifyFollowup(query, previousUserQuery string) string {
	if !isRetrievalFollowup(query) {
		return query
	}

	title, topic := searchutil.DocumentTitleHint(previousUserQuery)
	if title == "" || topic {
		return query
	}

	var context []string
	if strings.ContainsAny(query, "《》") {
		currentTitle, _ := searchutil.DocumentTitleHint(query)
		if currentTitle != title {
			return query
		}
	} else if explicitFilename.MatchString(query) {
		return query
	} else {
		context = append(context, "《"+title+"》")
	}

	if len(statedDimensions(query)) == 0 {
		if previous := statedDimensions(previousUserQuery); len(previous) == 1 {
			for dimension := range previous {
				context = append(context, dimension)
			}
		}
	}
	if len(context) == 0 {
		return query
	}
	return strings.Join(append(context, query), " ")
}

func isRetrievalFollowup(query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	for _, prefix := range []string{
		"那要是", "那如果", "那换成", "那改成", "如果换成", "如果改成",
		"换成", "改成", "同样", "what if we change", "what if we switch",
	} {
		if strings.HasPrefix(query, prefix) {
			return true
		}
	}
	return false
}

func statedDimensions(query string) map[string]struct{} {
	dimensions := make(map[string]struct{})
	for _, span := range dimensionExpression.FindAllStringIndex(query, -1) {
		// Reject embedded model codes such as AB50x6. A dimension may
		// touch a Chinese character or have units immediately after it.
		if span[0] > 0 {
			before := query[span[0]-1]
			if (before >= 'A' && before <= 'Z') || (before >= 'a' && before <= 'z') ||
				(before >= '0' && before <= '9') || before == '.' {
				continue
			}
		}
		value := strings.Join(strings.Fields(query[span[0]:span[1]]), "")
		dimensions[dimensionSeparator.Replace(value)] = struct{}{}
	}
	return dimensions
}
