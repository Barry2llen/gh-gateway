package graphqlapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

type statusCursor struct {
	Version       int    `json:"v"`
	Kind          string `json:"kind"`
	PullRequestID string `json:"pr"`
	HeadSHA       string `json:"sha"`
	AfterContext  string `json:"after"`
}

func encodeStatusCursor(prID, headSHA, after string) string {
	raw, _ := json.Marshal(statusCursor{Version: 1, Kind: "status-context", PullRequestID: prID, HeadSHA: headSHA, AfterContext: after})
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodeStatusCursor(value, prID, headSHA string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", errors.New("invalid status cursor")
	}
	var cursor statusCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Version != 1 || cursor.Kind != "status-context" || cursor.PullRequestID != prID || cursor.HeadSHA != headSHA || cursor.AfterContext == "" || encodeStatusCursor(cursor.PullRequestID, cursor.HeadSHA, cursor.AfterContext) != value {
		return "", errors.New("invalid status cursor")
	}
	return cursor.AfterContext, nil
}
