// Parts of this are stolen from:
// https://github.com/posener/h2conn/blob/master/server.go

package http2

import (
	"crypto/tls"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/go-micro/plugins/v4/transport/http2/ringbuffer"
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
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		flushWrite := &flushWrite{W: w, F: flusher}

		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		rb, _ := ringbuffer.CreateBuffer[*transport.Message](16, 1)

		h2Conn := &Http2Conn{
			options: s.options,
			local:   s.addr,
			buf:     make([]byte, 4*1024*1024),
			r:       r,
			w:       flushWrite,
			rb:      rb,
			encoder: gob.NewEncoder(flushWrite),
			decoder: gob.NewDecoder(r.Body),
		}

		acceptor(h2Conn)
	})

	server2 := &http2.Server{}
	for {
		conn, err := s.listener.Accept()

		switch conn.(type) {
		case *tls.Conn:
			if err := conn.(*tls.Conn).Handshake(); err != nil {
				fmt.Println(err)
				continue
			}
		}

		if err != nil {
			return err
		}

		server2.ServeConn(conn, &http2.ServeConnOpts{
			Handler: handler,
		})
	}
}

type Http2Conn struct {
	options *transport.Options
	once    sync.Once
	local   string
	r       *http.Request
	w       io.WriteCloser
	buf     []byte
	rb      ringbuffer.RingBuffer[*transport.Message]
	rbc     ringbuffer.Consumer[*transport.Message]
	encoder *gob.Encoder
	decoder *gob.Decoder
}

func (s *Http2Conn) readMessage(msg *transport.Message) error {
	s.options.Logger.Log(logger.TraceLevel, "readmessage")

	if err := s.decoder.Decode(msg); err != nil {
		s.options.Logger.Log(logger.ErrorLevel, err)
		return err
	}

	msg.Header[":path"] = s.r.URL.Path

	s.options.Logger.Log(logger.TraceLevel, "readmessage done")

	return nil
}

func (s *Http2Conn) writeMessage(msg *transport.Message) error {
	s.options.Logger.Log(logger.TraceLevel, "write message")

	if err := s.encoder.Encode(msg); err != nil {
		s.options.Logger.Log(logger.ErrorLevel, err)
		return err
	}

	s.options.Logger.Log(logger.TraceLevel, "write message done")
	return nil
}

func (s *Http2Conn) Recv(msg *transport.Message) error {
	if msg == nil {
		return errors.New("message passed in is nil")
	}

	var (
		err  error
		once = false
	)
	s.once.Do(func() {
		s.rbc, _ = s.rb.CreateConsumer()
		if err2 := s.readMessage(msg); err2 != nil {
			once = true
			err = err2
		}
		once = true
	})
	if err != nil || once {
		return err
	}

	s.options.Logger.Log(logger.TraceLevel, "wait ringbuffer")
	wMsg := s.rbc.Get()
	s.options.Logger.Log(logger.TraceLevel, "waited ringbuffer")

	if wMsg != nil {
		if err := s.writeMessage(wMsg); err != nil {
			return err
		}

		if err := s.readMessage(msg); err != nil {
			return err
		}
	} else {
		s.options.Logger.Log(logger.TraceLevel, "write message is nil")
		return fmt.Errorf("no message to send")
	}

	return nil
}

func (s *Http2Conn) Send(msg *transport.Message) error {
	if msg == nil {
		return errors.New("message passed in is nil")
	}

	s.options.Logger.Log(logger.TraceLevel, "have something to send")
	s.rb.Write(msg)

	return nil
}

func (s *Http2Conn) Close() error {
	s.options.Logger.Log(logger.TraceLevel, "closing")
	s.w.Close()
	return nil
}

func (s *Http2Conn) Local() string {
	return s.local
}

func (s *Http2Conn) Remote() string {
	return s.r.RemoteAddr
}
