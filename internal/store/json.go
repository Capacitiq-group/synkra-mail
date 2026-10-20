package store

import "encoding/json"

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("[]")
	}
	return b
}

func mustUnmarshalJSON(b []byte, v any) error {
	return json.Unmarshal(b, v)
}
