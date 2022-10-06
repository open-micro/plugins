// Parts of this are stolen from
// h2conn: https://github.com/posener/h2conn/blob/master/client.go

package http2

import (
	"context"
	"crypto/tls"
	"encoding/gob"
	"fmt"
	"io"
	"net/http"

	"github.com/go-micro/plugins/v4/transport/http2/ringbuffer"
	"go-micro.dev/v4/errors"
	"go-micro.dev/v4/logger"
	"go-micro.dev/v4/transport"
	"golang.org/x/net/http2"
)

func newHttp2Client(addr string, options *transport.Options) (*Http2Client, error) {
	c := &Http2Client{
		options:   options,
		addr:      addr,
		connected: false,
	}
	err := c.Dial()
	if err != nil {
		return nil, err
	}

	return c, nil
}

type Http2Client struct {
	options    *transport.Options
	client     *http.Client
	context    context.Context
	cancelFunc context.CancelFunc

	encoder *gob.Encoder
	decoder *gob.Decoder

	addr string

	connected bool

	rb  ringbuffer.RingBuffer[*transport.Message]
	rbc ringbuffer.Consumer[*transport.Message]
}

func (c *Http2Client) connect(urlStr string) error {
	reader, writer := io.Pipe()

	// Create a request object to send to the server
	req, err := http.NewRequest(http.MethodPost, urlStr, reader)
	if err != nil {
		return err
	}

	// Apply given context to the sent request
	c.context, c.cancelFunc = context.WithCancel(context.Background())
	req = req.WithContext(c.context)

	// Perform the request
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}

	// Check server status code
	if resp.StatusCode != http.StatusOK {
		c.options.Logger.Logf(logger.ErrorLevel, "bad status code: %d", resp.StatusCode)
		return errors.New("go.micro.transport.http2", "Bad Statuscode", int32(resp.StatusCode))
	}

	c.encoder = gob.NewEncoder(writer)
	c.decoder = gob.NewDecoder(resp.Body)

	return nil
}

func (c *Http2Client) Dial() error {
	c.client = &http.Client{
		Transport: &http2.Transport{
			DisableCompression: true,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	if err := c.connect(fmt.Sprintf("https://%s/", c.addr)); err != nil {
		c.options.Logger.Logf(logger.ErrorLevel, "initiate conn: %s", err)
		return err
	}

	c.rb, _ = ringbuffer.CreateBuffer[*transport.Message](8, 1)
	c.rbc, _ = c.rb.CreateConsumer()

	return nil
}

func (c *Http2Client) Send(msg *transport.Message) error {
	if msg == nil {
		return errors.New("go.micro.transport.http", "message passed in is nil", http.StatusInternalServerError)
	}

	c.options.Logger.Log(logger.TraceLevel, "writing message")
	c.rb.Write(msg)
	c.options.Logger.Log(logger.TraceLevel, "writing message done")

	return nil
}

func (c *Http2Client) Recv(msg *transport.Message) error {
	if msg == nil {
		return errors.New("go.micro.transport.http", "message passed in is nil", http.StatusInternalServerError)
	}

	req := c.rbc.Get()

	// Send request
	if err := c.encoder.Encode(req); err != nil {
		c.options.Logger.Log(logger.ErrorLevel, err)
		return fmt.Errorf("request: %w", err)
	}

	// Read response
	if err := c.decoder.Decode(msg); err != nil {
		c.options.Logger.Log(logger.ErrorLevel, err)
		return fmt.Errorf("read: %w", err)
	}

	return nil
}

func (c *Http2Client) Close() error {
	c.options.Logger.Log(logger.TraceLevel, "closing")
	c.cancelFunc()
	return nil
}

func (c *Http2Client) Local() string {
	return ""
}

func (c *Http2Client) Remote() string {
	return c.addr
}
