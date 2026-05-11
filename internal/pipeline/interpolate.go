package pipeline

import (
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	originToken    = "${ORIGIN}"
	uuidToken      = "${UUID}"
	timestampToken = "${TIMESTAMP}"
)

func interpolateRuntimeVariables(template string, origin string, now time.Time) string {
	var builder strings.Builder
	remaining := template
	for {
		index := nextRuntimeTokenIndex(remaining)
		if index < 0 {
			builder.WriteString(remaining)
			return builder.String()
		}
		builder.WriteString(remaining[:index])
		remaining = remaining[index:]
		switch {
		case strings.HasPrefix(remaining, originToken):
			builder.WriteString(origin)
			remaining = remaining[len(originToken):]
		case strings.HasPrefix(remaining, uuidToken):
			builder.WriteString(newUUID())
			remaining = remaining[len(uuidToken):]
		case strings.HasPrefix(remaining, timestampToken):
			builder.WriteString(strconv.FormatInt(now.Unix(), 10))
			remaining = remaining[len(timestampToken):]
		}
	}
}

func nextRuntimeTokenIndex(value string) int {
	index := -1
	for _, token := range []string{originToken, uuidToken, timestampToken} {
		tokenIndex := strings.Index(value, token)
		if tokenIndex >= 0 && (index < 0 || tokenIndex < index) {
			index = tokenIndex
		}
	}
	return index
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
