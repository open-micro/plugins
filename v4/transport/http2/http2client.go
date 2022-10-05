package http2

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"

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
	options *transport.Options
	client  http.Client

	addr string

	connected bool

	buf []byte

	rb  ringbuffer.RingBuffer[*http.Request]
	rbc ringbuffer.Consumer[*http.Request]
}

func (c *Http2Client) Dial() error {
	c.client = http.Client{Transport: &http2.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}}

	c.rb, _ = ringbuffer.CreateBuffer[*http.Request](256, 1)
	c.buf = make([]byte, 4*1024*1024)
	c.rbc, _ = c.rb.CreateConsumer()

	return nil
}

func (c *Http2Client) Send(msg *transport.Message) error {
	if msg == nil {
		return errors.New("go.micro.transport.http", "message passed in is nil", http.StatusInternalServerError)
	}

	// Prepare request
	header := make(http.Header, len(msg.Header))
	for k, v := range msg.Header {
		header.Set(k, v)
	}

	httpReq := &http.Request{
		Proto:  "HTTP/2.0",
		Method: http.MethodGet,
		URL: &url.URL{
			Scheme: "https",
			Host:   c.addr,
			Path:   "/",
		},
		Header:        header,
		ContentLength: int64(len(msg.Body)),
	}

	c.rb.Write(httpReq)

	return nil
}

func (c *Http2Client) Recv(msg *transport.Message) error {
	if msg == nil {
		return errors.New("go.micro.transport.http", "message passed in is nil", http.StatusInternalServerError)
	}

	req := c.rbc.Get()

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		logger.Error(err)
		return fmt.Errorf("request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return errors.New("go.micro.transport.http", resp.Status, int32(resp.StatusCode))
	}

	bufr := bufio.NewReader(resp.Body)
	n, err := bufr.Read(c.buf)
	msg.Body = c.buf[:n]
	if msg.Header == nil {
		msg.Header = make(map[string]string, len(resp.Header))
	}

	for k, v := range resp.Header {
		if len(v) > 0 {
			msg.Header[k] = v[0]
		} else {
			msg.Header[k] = ""
		}
	}

	if err != nil {
		logger.Error(err)
		return fmt.Errorf("read: %w", err)
	}

	return nil
}

func (c *Http2Client) Close() error {
	return nil
}

func (c *Http2Client) Local() string {
	return ""
}

func (c *Http2Client) Remote() string {
	return c.addr
}
