package upstream

import (
	"bytes"
	"context"
	"net/http"
)

func NewRequest(ctx context.Context, method, target string, header http.Header, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = header.Clone()
	return req, nil
}
