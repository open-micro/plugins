package mucp

import (
	proto "github.com/open-micro/plugins/v5/config/source/mucp/proto"
	"go-micro.org/v5/config/source"
)

type watcher struct {
	stream proto.Source_WatchService
}

func newWatcher(stream proto.Source_WatchService) (source.Watcher, error) {
	return &watcher{stream: stream}, nil
}

func (w *watcher) Next() (*source.ChangeSet, error) {
	var rsp proto.WatchResponse
	err := w.stream.RecvMsg(&rsp)
	if err != nil {
		return nil, err
	}
	return toChangeSet(rsp.ChangeSet), nil
}

func (w *watcher) Stop() error {
	return w.stream.Close()
}
