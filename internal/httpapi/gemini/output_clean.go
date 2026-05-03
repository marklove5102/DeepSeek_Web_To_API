package gemini

import "DeepSeek_Web_To_API/internal/httpapi/openai/shared"

//nolint:unused // retained for native Gemini output post-processing path.
func cleanVisibleOutput(text string, stripReferenceMarkers bool) string {
	return shared.CleanVisibleOutput(text, stripReferenceMarkers)
}
