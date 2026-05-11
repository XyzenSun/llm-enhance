package streaming

import (
	"fmt"
	"io"
	"net/http"

	"ai-api-stronger/internal/config"

	fakestream "github.com/xyzensun/llm-fakestream"
)

type FakeStreamer interface {
	Convert(w http.ResponseWriter, format string, resp *http.Response) error
}

type Boundary struct{ Streamer FakeStreamer }

func (b Boundary) Convert(w http.ResponseWriter, format string, resp *http.Response) error {
	if b.Streamer != nil {
		return b.Streamer.Convert(w, format, resp)
	}
	protocol, err := fakestreamProtocol(format)
	if err != nil {
		return err
	}
	bridge := fakestream.New(fakestream.Options{})
	return bridge.WriteResponse(w, protocol, resp)
}

func fakestreamProtocol(format string) (fakestream.Protocol, error) {
	switch format {
	case config.FormatOpenAI:
		return fakestream.ProtocolOpenAI, nil
	case config.FormatAnthropic:
		return fakestream.ProtocolAnthropic, nil
	default:
		return "", fmt.Errorf("unsupported fakestream format %q", format)
	}
}

type boundedReadCloser struct {
	r     io.Reader
	close func() error
}

func NewBoundedReadCloser(rc io.ReadCloser, limit int64) io.ReadCloser {
	if limit <= 0 {
		return rc
	}
	return &boundedReadCloser{
		r:     &io.LimitedReader{R: rc, N: limit + 1},
		close: rc.Close,
	}
}

func (b *boundedReadCloser) Read(p []byte) (int, error) {
	if lr, ok := b.r.(*io.LimitedReader); ok && lr.N <= 0 {
		return 0, ErrResponseTooLarge
	}
	n, err := b.r.Read(p)
	if lr, ok := b.r.(*io.LimitedReader); ok && lr.N <= 0 && err == nil {
		err = ErrResponseTooLarge
	}
	return n, err
}

func (b *boundedReadCloser) Close() error {
	if b.close == nil {
		return nil
	}
	return b.close()
}
