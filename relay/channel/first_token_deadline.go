package channel

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	common2 "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// firstTokenPrefetchLimit bounds how much non-data SSE text (comments, event
// names) is buffered while waiting for the first data event.
const firstTokenPrefetchLimit = 1 << 20

var errFirstTokenTimeout = errors.New("first token timeout")

// firstTokenDeadline fails an attempt whose upstream sends no data in time, so
// the relay can fall back to the next channel. A streaming attempt waits up to
// common.FirstTokenTimeoutSeconds for its first SSE data event. A
// non-streaming attempt, which answers only once the whole response is ready,
// waits up to common.NonStreamTimeoutSeconds for the first response byte. The
// wait covers the connection and the response headers too. When it fires the
// upstream request is cancelled and the relay gets a retryable error while
// nothing but keepalive pings has reached the client.
//
// It only arms while another channel can take over (RelayInfo.FallbackAvailable,
// set by the relay loop): the last channel, strict sessions and pinned channels
// keep waiting, because cutting them could only fail the request.
type firstTokenDeadline struct {
	timer   *time.Timer
	seconds int
	stream  bool
}

// startFirstTokenDeadline arms the deadline for a relay attempt and returns the
// request bound to the cancellable context. It returns nil when no other
// channel can take over or the matching timeout is disabled. Task submissions
// and channel tests never set FallbackAvailable, so they keep their own timing.
func startFirstTokenDeadline(info *common.RelayInfo, req *http.Request) (*firstTokenDeadline, *http.Request) {
	if info == nil || !info.FallbackAvailable {
		return nil, req
	}
	seconds := common2.NonStreamTimeoutSeconds
	if info.IsStream {
		seconds = common2.FirstTokenTimeoutSeconds
	}
	if seconds <= 0 {
		return nil, req
	}
	ctx, cancel := context.WithCancelCause(req.Context())
	deadline := &firstTokenDeadline{seconds: seconds, stream: info.IsStream}
	deadline.timer = time.AfterFunc(time.Duration(seconds)*time.Second, func() {
		cancel(errFirstTokenTimeout)
	})
	return deadline, req.WithContext(ctx)
}

// disarm stops the deadline once the upstream has answered. It reports false
// when the deadline already fired: the request is cancelled then, even if a
// response raced in. Call it once per attempt.
func (d *firstTokenDeadline) disarm() bool {
	if d == nil {
		return true
	}
	return d.timer.Stop()
}

// awaitFirstData waits for the first data of the response and puts what it read
// back in front of the remaining body, so adaptors parse the response
// unchanged. A successful plain SSE stream must deliver a data event: comments
// and event names alone do not count. Anything else, including error statuses
// and encoded bodies, counts its first byte. It reports false when the deadline
// fired first.
func (d *firstTokenDeadline) awaitFirstData(resp *http.Response) bool {
	reader := bufio.NewReader(resp.Body)
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	encoding := strings.ToLower(resp.Header.Get("Content-Encoding"))
	if !d.stream || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices ||
		!strings.Contains(contentType, "text/event-stream") || (encoding != "" && encoding != "identity") {
		// EOF and read errors surface to the adaptor as before, unless the read
		// failed because the deadline cancelled the request.
		_, _ = reader.Peek(1)
		resp.Body = prefetchedBody{Reader: reader, Closer: resp.Body}
		return d.disarm()
	}

	var prefetched bytes.Buffer
	for prefetched.Len() < firstTokenPrefetchLimit {
		line, err := reader.ReadBytes('\n')
		prefetched.Write(line)
		if payload, ok := bytes.CutPrefix(bytes.TrimLeft(line, " \t"), []byte("data:")); ok && len(bytes.TrimSpace(payload)) > 0 {
			break
		}
		if err != nil {
			break
		}
	}
	resp.Body = prefetchedBody{Reader: io.MultiReader(bytes.NewReader(prefetched.Bytes()), reader), Closer: resp.Body}
	return d.disarm()
}

// firstTokenTimeoutError is the retryable error for an attempt whose deadline
// fired. It carries an OpenAI error body so clients see the timeout code even
// after the relay handlers wrap it.
func firstTokenTimeoutError(c *gin.Context, seconds int) *types.NewAPIError {
	message := i18n.T(c, i18n.MsgRelayFirstTokenTimeout, map[string]any{"Seconds": seconds})
	return types.NewOpenAIError(errors.New(message), types.ErrorCodeFirstTokenTimeout, http.StatusGatewayTimeout)
}

type prefetchedBody struct {
	io.Reader
	io.Closer
}
