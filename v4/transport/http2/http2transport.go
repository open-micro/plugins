package http2

import (
	"sync"

	"go-micro.dev/v4/transport"
)

func NewTransport(opts ...transport.Option) transport.Transport {
	t := &Http2Transport{
		options: transport.Options{},
	}

	_ = t.Init(opts...)

	return t
}

type Http2Transport struct {
	options transport.Options
	once    sync.Once
}

func (t *Http2Transport) Init(opts ...transport.Option) error {
	t.once.Do(func() {
		for _, o := range opts {
			o(&t.options)
		}
	})

	return nil
}

func (t *Http2Transport) Options() transport.Options {
	return t.options
}

func (t *Http2Transport) Dial(addr string, opts ...transport.DialOption) (transport.Client, error) {
	return newHttp2Client(addr, &t.options)
}

func (t *Http2Transport) Listen(addr string, opts ...transport.ListenOption) (transport.Listener, error) {
	lOpts := transport.ListenOptions{}
	for _, o := range opts {
		o(&lOpts)
	}

	return newHttp2Server(addr, &t.options, lOpts)
}

func (t *Http2Transport) String() string {
	return "http2"
}
