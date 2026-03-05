package content

import (
	"context"
	"strings"
	"time"

	storageProvider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/tags"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
)

// Basic is the simplest Extractor implementation.
type Basic struct {
	logger log.Logger
}

// NewBasicExtractor creates a new Basic instance.
func NewBasicExtractor(logger log.Logger) (*Basic, error) {
	return &Basic{logger: logger}, nil
}

// Extract literally just rearranges the inputs and processes them into a Document.
func (b Basic) Extract(_ context.Context, ri *storageProvider.ResourceInfo) (Document, error) {
	doc := Document{
		Name:     ri.Name,
		Size:     ri.Size,
		MimeType: ri.MimeType,
	}

	if m := ri.ArbitraryMetadata.GetMetadata(); m != nil {
		if t, ok := m["tags"]; ok {
			doc.Tags = tags.New(t).AsSlice()
		}
		if t, ok := m[node.AllFavoritesKey]; ok && len(t) > 0 {
			doc.Favorites = strings.Split(t, ",")
		}
	}

	if ri.Mtime != nil {
		doc.Mtime = utils.TSToTime(ri.Mtime).UTC().Format(time.RFC3339Nano)
	}

	return doc, nil
}
