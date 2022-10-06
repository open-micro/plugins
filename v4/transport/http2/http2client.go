// Parts of this are stolen from
// h2conn: https://github.com/posener/h2conn/blob/master/client.go

package http2

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"

	"go-micro.dev/v4/errors"
	"go-micro.dev/v4/logger"
	"go-micro.dev/v4/transport"
	"golang.org/x/net/http2"
)

func newHttp2Client(addr string, options *transport.Options) (*Http2Client, error) {
	c := &Http2Client{
		options: options,
		addr:    addr,
	}
	err := c.Dial()
	if err != nil {
		return nil, err
	}

	return c, nil
}

type Http2Client struct {
	options *transport.Options
	client  *http.Client
	conn    *Conn

	addr string

	mUnmarshaler mUnmarshaler
}

func (c *Http2Client) connect(urlStr string) error {
	reader, writer := io.Pipe()

	// Create a request object to send to the server
	req, err := http.NewRequest(http.MethodPost, urlStr, reader)
	if err != nil {
		return err
	}

	// Apply given context to the sent request
	ctx := context.Background()
	req = req.WithContext(ctx)

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

	conn, ctx := newConn(req.Context(), resp.Body, writer)
	resp.Request = req.WithContext(ctx)
	c.conn = conn

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

	c.options.Logger.Log(logger.DebugLevel, "connecting")

	if err := c.connect(fmt.Sprintf("https://%s/", c.addr)); err != nil {
		c.options.Logger.Logf(logger.ErrorLevel, "initiate conn: %s", err)
		return err
	}

	c.mUnmarshaler, _ = NewGobMUnmarshaler(c.conn, c.conn)

	return nil
}

func (c *Http2Client) Send(msg *transport.Message) error {
	if msg == nil {
		return errors.New("go.micro.transport.http", "message passed in is nil", http.StatusInternalServerError)
	}

	c.options.Logger.Logf(logger.TraceLevel, "sending message")

	// Send request
	c.options.Logger.Log(logger.TraceLevel, "writing")
	err := c.mUnmarshaler.Marshal(msg)
	if err != nil {
		c.options.Logger.Log(logger.ErrorLevel, err)
		return err
	}
	c.options.Logger.Log(logger.TraceLevel, "writing done")

	return nil
}

func (c *Http2Client) Recv(msg *transport.Message) error {
	if msg == nil {
		return errors.New("go.micro.transport.http", "message passed in is nil", http.StatusInternalServerError)
	}

	// Read response
	c.options.Logger.Log(logger.TraceLevel, "reading")
	if err := c.mUnmarshaler.Unmarshal(msg); err != nil {
		c.options.Logger.Log(logger.ErrorLevel, err)
		return err
	}
	c.options.Logger.Log(logger.TraceLevel, "reading done")

	return nil
}

func (c *Http2Client) Close() error {
	c.options.Logger.Log(logger.TraceLevel, "closing")
	c.conn.Close()
	return nil
}

func (c *Http2Client) Local() string {
	return ""
}

func (c *Http2Client) Remote() string {
	return c.addr
}
