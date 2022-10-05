package http2

import (
	"bufio"
	"crypto/tls"
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

	bufrb ringbuffer.RingBuffer[[]byte]
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
		if r.ProtoMajor != 2 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			s.options.Logger.Log(logger.ErrorLevel, "Not a post?")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, ok := w.(http.Flusher); !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		bufr := bufio.NewReader(r.Body)

		rb, _ := ringbuffer.CreateBuffer[*transport.Message](256, 10)

		h2Conn := &Http2Conn{
			options: s.options,
			local:   s.addr,
			r:       r,
			w:       w,
			buf:     make([]byte, 4*1024*1024),
			bufr:    bufr,
			rb:      rb,
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
	local   string
	r       *http.Request
	w       http.ResponseWriter

	closed bool
	once   sync.Once

	buf  []byte
	bufr *bufio.Reader

	ringbuffer.RingBuffer[[]byte]

	rb  ringbuffer.RingBuffer[*transport.Message]
	rbc ringbuffer.Consumer[*transport.Message]
}

func (s *Http2Conn) readMessage(msg *transport.Message) error {
	s.options.Logger.Log(logger.TraceLevel, "readmessage")

	// Read request
	if msg.Header == nil {
		msg.Header = make(map[string]string, len(s.r.Header))
	}
	for k, v := range s.r.Header {
		if len(v) > 0 {
			msg.Header[k] = v[0]
		} else {
			msg.Header[k] = ""
		}
	}

	msg.Header[":path"] = s.r.URL.Path

	s.options.Logger.Logf(logger.TraceLevel, "ContentLength: %d", s.r.ContentLength)

	n, err := s.bufr.Read(s.buf)
	msg.Body = s.buf[:n]
	if err != nil {
		if err == io.EOF {
			s.options.Logger.Log(logger.TraceLevel, "read eof")
			return nil
		}
		s.options.Logger.Log(logger.ErrorLevel, err)
		return err
	}

	s.options.Logger.Log(logger.TraceLevel, "readmessage done")

	return nil
}

func (s *Http2Conn) writeMessage(msg *transport.Message) error {
	s.options.Logger.Log(logger.TraceLevel, "write message")
	// Write response
	for k, v := range msg.Header {
		s.w.Header().Set(k, v)
	}

	_, err := s.w.Write(msg.Body)
	if err != nil {
		if err == io.EOF {
			s.options.Logger.Log(logger.TraceLevel, "write eof")
			return nil
		}

		s.options.Logger.Log(logger.ErrorLevel, err)
		return err
	}

	// flush the trailers
	s.w.(http.Flusher).Flush()

	s.options.Logger.Log(logger.TraceLevel, "write message done")
	return nil
}

func (s *Http2Conn) Recv(msg *transport.Message) error {
	if msg == nil {
		return errors.New("message passed in is nil")
	}

	if s.closed {
		return io.EOF
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

	if s.closed {
		return io.EOF
	}

	s.options.Logger.Log(logger.TraceLevel, "have something to send")
	s.rb.Write(msg)

	return nil
}

func (s *Http2Conn) Close() error {
	s.options.Logger.Log(logger.TraceLevel, "closing")
	s.r.Body.Close()
	s.rb.Write(nil)
	s.closed = true

	return nil
}

func (s *Http2Conn) Local() string {
	return s.local
}

func (s *Http2Conn) Remote() string {
	return s.r.RemoteAddr
}
