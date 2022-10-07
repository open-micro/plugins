// Parts of this are stolen from:
// https://github.com/posener/h2conn/blob/master/server.go

package http2

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"go-micro.dev/v4/logger"
	"go-micro.dev/v4/transport"
	"golang.org/x/net/http2"

	maddr "go-micro.dev/v4/util/addr"
	mnet "go-micro.dev/v4/util/net"
	mls "go-micro.dev/v4/util/tls"
)

type flushWrite struct {
	W io.Writer
	F http.Flusher
}

func (w *flushWrite) Write(data []byte) (int, error) {
	n, err := w.W.Write(data)
	w.F.Flush()
	return n, err
}

func (w *flushWrite) Close() error {
	// Currently server side close of connection is not supported in Go.
	// The server closes the connection when the http.Handler function returns.
	// We use connection context and cancel function as a work-around.
	return nil
}

func newHttp2Server(addr string, options *transport.Options, lopts transport.ListenOptions) (*Http2Server, error) {
	s := &Http2Server{options: options}

	if err := s.Listen(addr, lopts); err != nil {
		return nil, err
	}

	return s, nil
}

type Http2Server struct {
	options   *transport.Options
	tlsConfig *tls.Config
	addr      string
	listener  net.Listener
}

func (s *Http2Server) Listen(addr string, lopts transport.ListenOptions) error {
	var (
		list net.Listener
		err  error
	)

	if s.options.Secure || s.options.TLSConfig != nil {
		config := s.options.TLSConfig

		fn := func(addr string) (net.Listener, error) {
			if config == nil {
				hosts := []string{addr}

				// check if its a valid host:port
				if host, _, err := net.SplitHostPort(addr); err == nil {
					if len(host) == 0 {
						hosts = maddr.IPs()
					} else {
						hosts = []string{host}
					}
				}

				// generate a certificate
				cert, err := mls.Certificate(hosts...)
				if err != nil {
					return nil, err
				}
				config = &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{http2.NextProtoTLS}}
				s.tlsConfig = config
			}
			return tls.Listen("tcp", addr, config)
		}

		list, err = mnet.Listen(addr, fn)
	} else {
		fn := func(addr string) (net.Listener, error) {
			return net.Listen("tcp", addr)
		}

		list, err = mnet.Listen(addr, fn)
	}

	if err != nil {
		return fmt.Errorf("error while listening, error was: %w", err)
	}

	s.addr = list.Addr().String()
	s.listener = list

	return nil
}

func (s *Http2Server) Addr() string {
	return s.listener.Addr().String()
}

func (s *Http2Server) Close() error {
	s.listener.Close()
	return nil
}

func (s *Http2Server) Accept(acceptor func(transport.Socket)) error {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.ProtoAtLeast(2, 0) {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		s.options.Logger.Logf(logger.DebugLevel, "New connection from: %s", r.RemoteAddr)

		c, ctx := newConn(r.Context(), r.Body, &flushWrite{W: w, F: flusher})

		// Update the request context with the connection context.
		// If the connection is closed by the server, it will also notify everything that waits on the request context.
		*r = *r.WithContext(ctx)

		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		mu, _ := NewGobMUnmarshaler(c, c)

		h2Conn := &Http2Conn{
			options:      s.options,
			local:        s.addr,
			r:            r,
			conn:         c,
			mUnarmshaler: mu,
		}

		acceptor(h2Conn)
	})

	server2 := &http2.Server{}
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return err
		}

		go func(conn net.Conn) {
			switch conn.(type) {
			case *tls.Conn:
				if err := conn.(*tls.Conn).Handshake(); err != nil {
					return
				}
			}

			server2.ServeConn(conn, &http2.ServeConnOpts{
				Handler: handler,
			})
		}(conn)
	}
}

type Http2Conn struct {
	options *transport.Options
	local   string
	r       *http.Request
	conn    *Conn

	mUnarmshaler mUnmarshaler
}

func (s *Http2Conn) readMessage(msg *transport.Message) error {
	s.options.Logger.Logf(logger.TraceLevel, "readmessage for: %s", s.Remote())

	err := s.mUnarmshaler.Unmarshal(msg)
	if err != nil {
		if err == io.EOF {
			return nil
		}

		s.options.Logger.Log(logger.ErrorLevel, err)
		return err
	}

	msg.Header[":path"] = s.r.URL.Path

	s.options.Logger.Logf(logger.TraceLevel, "readmessage done, for: %s", s.Remote())

	return nil
}

func (s *Http2Conn) writeMessage(msg *transport.Message) error {
	s.options.Logger.Logf(logger.TraceLevel, "[%s] writemessage", s.Remote())

	err := s.mUnarmshaler.Marshal(msg)
	if err != nil {
		s.options.Logger.Log(logger.ErrorLevel, err)
		return err
	}

	s.options.Logger.Logf(logger.TraceLevel, "[%s] writemessage done", s.Remote())
	return nil
}

func (s *Http2Conn) Recv(msg *transport.Message) error {
	if msg == nil {
		return errors.New("message passed in is nil")
	}

	return s.readMessage(msg)
}

func (s *Http2Conn) Send(msg *transport.Message) error {
	if msg == nil {
		return errors.New("message passed in is nil")
	}

	if err := s.writeMessage(msg); err != nil {
		return err
	}

	return s.writeMessage(msg)
}

func (s *Http2Conn) Close() error {
	s.options.Logger.Log(logger.TraceLevel, "closing")
	s.conn.Close()
	return nil
}

func (s *Http2Conn) Local() string {
	return s.local
}

func (s *Http2Conn) Remote() string {
	return s.r.RemoteAddr
}
