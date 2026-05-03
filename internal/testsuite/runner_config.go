package testsuite

import (
	"encoding/json"
	"os"
	"strings"
)

func adminKeyFromConfig(path string) string {
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return ""
	}
	var doc struct {
		Admin struct {
			Key string `json:"key"`
		} `json:"admin"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Admin.Key)
}
