package bleve

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	storageProvider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/search/pkg/search"

	searchMessage "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/messages/search/v0"
	searchService "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	searchQuery "github.com/opencloud-eu/opencloud/services/search/pkg/query"
)

const defaultBatchSize = 50

var _ search.Engine = (*Backend)(nil) // ensure Backend implements Engine

type Backend struct {
	index        bleve.Index
	queryCreator searchQuery.Creator[query.Query]
	log          log.Logger
}

func NewBackend(index bleve.Index, queryCreator searchQuery.Creator[query.Query], log log.Logger) *Backend {
	return &Backend{
		index:        index,
		queryCreator: queryCreator,
		log:          log,
	}
}

// Search executes a search request operation within the index.
// Returns a SearchIndexResponse object or an error.
func (b *Backend) Search(_ context.Context, sir *searchService.SearchIndexRequest) (*searchService.SearchIndexResponse, error) {
	createdQuery, err := b.queryCreator.Create(sir.Query)
	if err != nil {
		if searchQuery.IsValidationError(err) {
			return nil, errtypes.BadRequest(err.Error())
		}
		return nil, err
	}

	// Exclude documents that have been explicitly marked as deleted.
	// We use a negation (MustNot) rather than matching Deleted==false,
	// because Bleve does not index zero-value bools by default.
	deletedQuery := bleve.NewBooleanQuery()
	deletedQuery.AddMustNot(&query.BoolFieldQuery{
		Bool:     true,
		FieldVal: "Deleted",
	})

	q := bleve.NewConjunctionQuery(
		deletedQuery,
		createdQuery,
	)

	if sir.Ref != nil {
		q.Conjuncts = append(
			q.Conjuncts,
			&query.TermQuery{
				FieldVal: "RootID",
				Term: storagespace.FormatResourceID(
					&storageProvider.ResourceId{
						StorageId: sir.Ref.GetResourceId().GetStorageId(),
						SpaceId:   sir.Ref.GetResourceId().GetSpaceId(),
						OpaqueId:  sir.Ref.GetResourceId().GetOpaqueId(),
					},
				),
			},
		)
	}

	bleveReq := bleve.NewSearchRequest(q)
	bleveReq.Highlight = bleve.NewHighlight()

	switch {
	case sir.PageSize == -1:
		bleveReq.Size = math.MaxInt
	case sir.PageSize == 0:
		bleveReq.Size = 200
	default:
		bleveReq.Size = int(sir.PageSize)
	}

	bleveReq.Fields = []string{"*"}
	wallStart := time.Now()
	res, err := b.index.Search(bleveReq)
	wallDur := time.Since(wallStart)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "bleve-search: took=%v wall=%v hits=%d segments=%d query=%s\n", res.Took, wallDur, res.Total, 0, sir.Query)

	matches := make([]*searchMessage.Match, 0, len(res.Hits))
	totalMatches := res.Total
	for _, hit := range res.Hits {
		if sir.Ref != nil {
			hitPath := strings.TrimSuffix(getFieldString(hit.Fields, "Path"), "/")
			requestedPath := utils.MakeRelativePath(sir.Ref.Path)
			isRoot := hitPath == requestedPath

			if !isRoot && requestedPath != "." && !strings.HasPrefix(hitPath, requestedPath+"/") {
				totalMatches--
				continue
			}
		}

		rootID, err := storagespace.ParseID(getFieldString(hit.Fields, "RootID"))
		if err != nil {
			return nil, err
		}

		rID, err := storagespace.ParseID(getFieldString(hit.Fields, "ID"))
		if err != nil {
			return nil, err
		}

		pID, _ := storagespace.ParseID(getFieldString(hit.Fields, "ParentID"))
		// Extract Metadata.* fields from the flat bleve field map
		metadata := make(map[string]string)
		for k, v := range hit.Fields {
			kLower := strings.ToLower(k)
			if strings.HasPrefix(kLower, "metadata.") {
				if s, ok := v.(string); ok {
					metadata[k[len("metadata."):]] = s
				}
			}
		}
		if len(metadata) == 0 {
			metadata = nil
		}

		// Sanitize metadata values (gRPC marshaling fails on invalid UTF-8)
		if metadata != nil {
			for k, v := range metadata {
				if !utf8.ValidString(v) {
					metadata[k] = strings.ToValidUTF8(v, "\uFFFD")
				}
			}
		}

		match := &searchMessage.Match{
			Score: float32(hit.Score),
			Entity: &searchMessage.Entity{
				Ref: &searchMessage.Reference{
					ResourceId: resourceIDtoSearchID(rootID),
					Path:       getFieldString(hit.Fields, "Path"),
				},
				Id:         resourceIDtoSearchID(rID),
				Name:       getFieldString(hit.Fields, "Name"),
				ParentId:   resourceIDtoSearchID(pID),
				Size:       uint64(getFieldValue[float64](hit.Fields, "Size")),
				Type:       uint64(getFieldValue[float64](hit.Fields, "Type")),
				MimeType:   getFieldString(hit.Fields, "MimeType"),
				Deleted:    getFieldValue[bool](hit.Fields, "Deleted"),
				Tags:       getFieldSliceValue[string](hit.Fields, "Tags"),
				Favorites:  getFieldSliceValue[string](hit.Fields, "Favorites"),
				Highlights: sanitizeUTF8(buildHighlights(hit.Fragments, hit.Fields, sir.Query)),
				Audio:      getAudioValue[searchMessage.Audio](hit.Fields),
				Image:      getImageValue[searchMessage.Image](hit.Fields),
				Location:   getLocationValue[searchMessage.GeoCoordinates](hit.Fields),
				Photo:      getPhotoValue[searchMessage.Photo](hit.Fields),
				Metadata:   metadata,
			},
		}

		if mtime, err := time.Parse(time.RFC3339, getFieldString(hit.Fields, "Mtime")); err == nil {
			match.Entity.LastModifiedTime = &timestamppb.Timestamp{Seconds: mtime.Unix(), Nanos: int32(mtime.Nanosecond())}
		}

		matches = append(matches, match)
	}

	return &searchService.SearchIndexResponse{
		Matches:      matches,
		TotalMatches: int32(totalMatches),
	}, nil
}

func (b *Backend) DocCount() (uint64, error) {
	return b.index.DocCount()
}

// StatsMap returns internal Scorch index statistics (segments, merges, persister, etc.).
func (b *Backend) StatsMap() map[string]interface{} {
	return b.index.StatsMap()
}

func (b *Backend) Upsert(id string, r search.Resource) error {
	batch, err := b.NewBatch(defaultBatchSize)
	if err != nil {
		return err
	}

	if err := batch.Upsert(id, r); err != nil {
		return err
	}

	return batch.Push()
}

func (b *Backend) Move(rootID, parentID, location string) error {
	batch, err := b.NewBatch(defaultBatchSize)
	if err != nil {
		return err
	}

	if err := batch.Move(rootID, parentID, location); err != nil {
		return err
	}

	return batch.Push()
}

func (b *Backend) Delete(id string) error {
	batch, err := b.NewBatch(defaultBatchSize)
	if err != nil {
		return err
	}

	if err := batch.Delete(id); err != nil {
		return err
	}

	return batch.Push()
}

func (b *Backend) Restore(id string) error {
	batch, err := b.NewBatch(defaultBatchSize)
	if err != nil {
		return err
	}

	if err := batch.Restore(id); err != nil {
		return err
	}

	return batch.Push()
}

func (b *Backend) Purge(id string, onlyDeleted bool) error {
	batch, err := b.NewBatch(defaultBatchSize)
	if err != nil {
		return err
	}

	if err := batch.Purge(id, onlyDeleted); err != nil {
		return err
	}

	return batch.Push()
}

func (b *Backend) NewBatch(size int) (search.BatchOperator, error) {
	return NewBatch(b.index, size)
}

// ResolvePathID resolves a resource ID to its current location in the Bleve index.
// First tries a direct ID lookup, then falls back to OldIDs (cross-space move).
// Returns the current Resource (with ID, Path, RootID) or an error if not found.
func (b *Backend) ResolvePathID(id string) (*search.Resource, error) {
	// 1. Direct lookup by document ID
	doc, err := b.index.Document(id)
	if err == nil && doc != nil {
		req := bleve.NewSearchRequest(bleve.NewDocIDQuery([]string{id}))
		req.Fields = []string{"*"}
		req.Size = 1
		res, err := b.index.Search(req)
		if err == nil && res.Hits.Len() > 0 {
			return matchToResource(res.Hits[0]), nil
		}
	}

	// 2. OldIDs fallback (cross-space move)
	q := bleve.NewTermQuery(id)
	q.SetField("OldIDs")

	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1

	res, err := b.index.Search(req)
	if err != nil {
		return nil, err
	}
	if res.Hits.Len() == 0 {
		return nil, errtypes.NotFound("resource ID not found in index: " + id)
	}

	resource := matchToResource(res.Hits[0])

	// The found resource might itself have been moved again — follow the chain
	// by checking if this resource's current ID is referenced as an OldID elsewhere.
	// Max 100 hops to prevent loops.
	for i := 0; i < 100; i++ {
		nextQ := bleve.NewTermQuery(resource.ID)
		nextQ.SetField("OldIDs")
		nextReq := bleve.NewSearchRequest(nextQ)
		nextReq.Fields = []string{"*"}
		nextReq.Size = 1
		nextRes, err := b.index.Search(nextReq)
		if err != nil || nextRes.Hits.Len() == 0 {
			break
		}
		resource = matchToResource(nextRes.Hits[0])
	}

	return resource, nil
}

// sanitizeUTF8 replaces invalid UTF-8 bytes with U+FFFD.
// Prevents gRPC marshaling errors ("string field contains invalid UTF-8").
func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "\uFFFD")
}
