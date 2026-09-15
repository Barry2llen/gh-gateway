package graphqlapi

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

const pullRequestIDPrefix = "PullRequest:"

func encodeOpaqueID(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func encodePullRequestID(owner, repo string, number int64) string {
	return encodeOpaqueID(pullRequestIDPrefix + owner + "/" + repo + "/" + strconv.FormatInt(number, 10))
}

func decodePullRequestID(id string) (string, string, int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || encodeOpaqueID(string(raw)) != id || !strings.HasPrefix(string(raw), pullRequestIDPrefix) {
		return "", "", 0, errors.New("invalid PullRequest ID")
	}
	parts := strings.Split(strings.TrimPrefix(string(raw), pullRequestIDPrefix), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return "", "", 0, errors.New("invalid PullRequest ID")
	}
	number, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || number <= 0 {
		return "", "", 0, errors.New("invalid PullRequest ID")
	}
	return parts[0], parts[1], number, nil
}
