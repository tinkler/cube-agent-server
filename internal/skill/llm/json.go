package llm

import "encoding/json"

// unmarshalJSON 解析 JSON 字符串到 out
func unmarshalJSON(s string, out any) error {
	return json.Unmarshal([]byte(s), out)
}
