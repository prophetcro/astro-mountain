package meteoblue

import (
	"bytes"
	"encoding/json"
)

// ParseResponse 把 Meteoblue 原始 JSON 解码为 MetoResponse。
// 缺测（null）元素由 Data1h 的指针字段自然吸收，不会因个别时次缺测而整段失败。
func ParseResponse(body []byte) (*MetoResponse, error) {
	var resp MetoResponse
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
