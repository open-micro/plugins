package quic

import (
	"context"
	"crypto/tls"
	"encoding/gob"
	"time"

	quic "github.com/lucas-clemente/quic-go"
	"go-micro.dev/v4/transport"
	"go-micro.dev/v4/util/cmd"
	utls "go-micro.dev/v4/util/tls"
)

type quicSocket struct {
	conn quic.Connection
	st   quic.Stream
	enc  *gob.Encoder
	dec  *gob.Decoder
}

type quicTransport struct {
	opts transport.Options
}

type quicClient struct {
	*quicSocket
	t    *quicTransport
	opts transport.DialOptions
}

type quicListener struct {
	l    quic.Listener
	t    *quicTransport
	opts transport.ListenOptions
}

func init() {
	cmd.DefaultTransports["quic"] = NewTransport
}

func (q *quicClient) Close() error {
	return q.quicSocket.st.Close()
}

func (q *quicSocket) Recv(m *transport.Message) error {
	return q.dec.Decode(&m)
}

func (q *quicSocket) Send(m *transport.Message) error {
	// set the write deadline
	q.st.SetWriteDeadline(time.Now().Add(time.Second * 10))
	// send the data
	return q.enc.Encode(m)
}

func (q *quicSocket) Close() error {
	return q.conn.CloseWithError(0, "EOF")
}

func (q *quicSocket) Local() string {
	return q.conn.LocalAddr().String()
}

func (q *quicSocket) Remote() string {
	return q.conn.RemoteAddr().String()
}

func (q *quicListener) Addr() string {
	return q.l.Addr().String()
}

func (q *quicListener) Close() error {
	return q.l.Close()
}

func (q *quicListener) Accept(fn func(transport.Socket)) error {
	for {
		conn, err := q.l.Accept(context.TODO())
		if err != nil {
			return err
		}

		stream, err := conn.AcceptStream(context.TODO())
		if err != nil {
			continue
		}

		go func() {
			fn(&quicSocket{
				conn: conn,
				st:   stream,
				enc:  gob.NewEncoder(stream),
				dec:  gob.NewDecoder(stream),
			})
		}()
	}
}

func (q *quicTransport) Init(opts ...transport.Option) error {
	for _, o := range opts {
		o(&q.opts)
	}
	return nil
}

func (q *quicTransport) Options() transport.Options {
	return q.opts
}

func (q *quicTransport) Dial(addr string, opts ...transport.DialOption) (transport.Client, error) {
	var options transport.DialOptions
	for _, o := range opts {
		o(&options)
	}

	tlsConfig := q.opts.TLSConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"http/1.1"},
		}
	}
	conn, err := quic.DialAddr(addr, tlsConfig, &quic.Config{
		MaxIdleTimeout: time.Minute * 2,
	})
	if err != nil {
		return nil, err
	}

	st, err := conn.OpenStreamSync(context.TODO())
	if err != nil {
		return nil, err
	}

	enc := gob.NewEncoder(st)
	dec := gob.NewDecoder(st)

	return &quicClient{
		&quicSocket{
			conn: conn,
			st:   st,
			enc:  enc,
			dec:  dec,
		},
		q,
		options,
	}, nil
}

func (q *quicTransport) Listen(addr string, opts ...transport.ListenOption) (transport.Listener, error) {
	var options transport.ListenOptions
	for _, o := range opts {
		o(&options)
	}

	config := q.opts.TLSConfig
	if config == nil {
		cfg, err := utls.Certificate(addr)
		if err != nil {
			return nil, err
		}
		config = &tls.Config{
			Certificates: []tls.Certificate{cfg},
			NextProtos:   []string{"http/1.1"},
		}
	}

	l, err := quic.ListenAddr(addr, config, nil)
	if err != nil {
		return nil, err
	}

	return &quicListener{
		l:    l,
		t:    q,
		opts: options,
	}, nil
}

func (q *quicTransport) String() string {
	return "quic"
}

func NewTransport(opts ...transport.Option) transport.Transport {
	options := transport.Options{}

	for _, o := range opts {
		o(&options)
	}

	return &quicTransport{
		opts: options,
	}
}
