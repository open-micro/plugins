package grpc

import (
	"time"

	proto "github.com/open-micro/plugins/v5/config/source/grpc/proto"
	"go-micro.org/v5/config/source"
)

func toChangeSet(c *proto.ChangeSet) *source.ChangeSet {
	return &source.ChangeSet{
		Data:      c.Data,
		Checksum:  c.Checksum,
		Format:    c.Format,
		Timestamp: time.Unix(c.Timestamp, 0),
		Source:    c.Source,
	}
}
