package shared

import textclean "DeepSeek_Web_To_API/internal/textclean"

func CleanVisibleOutput(text string, stripReferenceMarkers bool) string {
	if text == "" {
		return text
	}
	if stripReferenceMarkers {
		text = textclean.StripReferenceMarkers(text)
	}
	return sanitizeLeakedOutput(text)
}

func CleanVisibleOutputPreservingToolMarkup(text string, stripReferenceMarkers bool) string {
	if text == "" {
		return text
	}
	if stripReferenceMarkers {
		text = textclean.StripReferenceMarkers(text)
	}
	return sanitizeLeakedOutputPreservingToolMarkup(text)
}
