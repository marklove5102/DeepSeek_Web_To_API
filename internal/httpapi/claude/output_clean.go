package claude

import "DeepSeek_Web_To_API/internal/httpapi/openai/shared"

func cleanVisibleOutput(text string, stripReferenceMarkers bool) string {
	return shared.CleanVisibleOutput(text, stripReferenceMarkers)
}

func cleanVisibleOutputPreservingToolMarkup(text string, stripReferenceMarkers bool) string {
	return shared.CleanVisibleOutputPreservingToolMarkup(text, stripReferenceMarkers)
}
