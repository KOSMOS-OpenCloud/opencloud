package bleve

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/keyword"
	"github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	"github.com/blevesearch/bleve/v2/analysis/token/porter"
	"github.com/blevesearch/bleve/v2/analysis/tokenizer/single"
	"github.com/blevesearch/bleve/v2/analysis/tokenizer/unicode"
	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/blevesearch/bleve/v2/mapping"
	storageProvider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"

	"github.com/opencloud-eu/opencloud/services/search/pkg/search"
)

// mergerDead is set to 1 when the scorch merger goroutine fires an async
// error (typically a panic). Once set it never clears — the index must be
// considered degraded until the process is restarted.
var mergerDead atomic.Int32

// mergerDeadSince records the wall-clock time of the first async error.
var mergerDeadSince atomic.Pointer[time.Time]

// MergerIsDead returns true after the scorch merger goroutine has died.
func MergerIsDead() bool { return mergerDead.Load() != 0 }

// MergerDeadSince returns the time the merger died, or zero if alive.
func MergerDeadSince() time.Time {
	if t := mergerDeadSince.Load(); t != nil {
		return *t
	}
	return time.Time{}
}

func init() {
	scorch.RegistryAsyncErrorCallbacks["log"] = func(err error, path string) {
		now := time.Now()
		mergerDead.Store(1)
		mergerDeadSince.CompareAndSwap(nil, &now)
		fmt.Fprintf(os.Stderr,
			"\n*** SEARCH ALARM: scorch merger goroutine died ***\n"+
				"    path:  %s\n"+
				"    error: %v\n"+
				"    time:  %s\n"+
				"    Bleve can no longer merge segments. Segment count will grow\n"+
				"    unbounded until writes block at 200 segments. The search\n"+
				"    index is degraded — a process restart is required.\n\n",
			path, err, now.Format(time.RFC3339))
	}
}

func NewIndex(root string, persisterNapTimeMs, persisterNapUnderNumFiles int) (bleve.Index, error) {
	destination := filepath.Join(root, "bleve")

	kvconfig := map[string]interface{}{
		"asyncErrorCallbackName": "log",
		// Merge tuning: smaller tasks to reduce peak memory during merge.
		// Defaults: SegmentsPerMergeTask=10, MaxSegmentSize=5000000
		"scorchMergePlanOptions": map[string]interface{}{
			"MaxSegmentsPerTier":   10,
			"MaxSegmentSize":       int64(2000000),
			"SegmentsPerMergeTask": 4,
			"TierGrowth":           10.0,
			"FloorSegmentSize":     int64(2000),
			"ReclaimDeletesWeight": 2.0,
		},
	}
	if persisterNapTimeMs > 0 {
		kvconfig["scorchPersisterOptions"] = map[string]interface{}{
			"PersisterNapTimeMSec":      persisterNapTimeMs,
			"PersisterNapUnderNumFiles": persisterNapUnderNumFiles,
		}
	}

	index, err := bleve.OpenUsing(destination, kvconfig)
	if err == nil {
		return index, nil
	}

	if !errors.Is(bleve.ErrorIndexPathDoesNotExist, err) {
		// Index exists but is corrupt (e.g. empty mapping after crash).
		// Remove and recreate.
		fmt.Fprintf(os.Stderr, "search: corrupt bleve index at %s, recreating: %v\n", destination, err)
		os.RemoveAll(destination)
	}

	indexMapping, err := NewMapping()
	if err != nil {
		return nil, err
	}
	index, err = bleve.NewUsing(destination, indexMapping, "scorch", "scorch", kvconfig)
	if err != nil {
		return nil, err
	}

	return index, nil
}

func NewMapping() (mapping.IndexMapping, error) {
	nameMapping := bleve.NewTextFieldMapping()
	nameMapping.Analyzer = "lowercaseKeyword"

	lowercaseMapping := bleve.NewTextFieldMapping()
	lowercaseMapping.IncludeInAll = false
	lowercaseMapping.Analyzer = "lowercaseKeyword"

	fulltextFieldMapping := bleve.NewTextFieldMapping()
	fulltextFieldMapping.Analyzer = "fulltext"
	fulltextFieldMapping.IncludeInAll = false

	// Metadata fields are handled by the default document mapping with
	// StoreDynamic=true on the IndexMapping. This ensures Metadata.* fields
	// are both indexed (searchable) AND stored (returned in hit.Fields).
	// A separate SubDocumentMapping would override StoreDynamic inheritance.

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("Name", nameMapping)
	docMapping.AddFieldMappingsAt("Tags", lowercaseMapping)
	docMapping.AddFieldMappingsAt("Favorites", lowercaseMapping)
	docMapping.AddFieldMappingsAt("Content", fulltextFieldMapping)

	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultAnalyzer = keyword.Name
	indexMapping.StoreDynamic = true // ensure Metadata.* fields are returned in hit.Fields
	indexMapping.DefaultMapping = docMapping
	err := indexMapping.AddCustomAnalyzer("lowercaseKeyword",
		map[string]any{
			"type":      custom.Name,
			"tokenizer": single.Name,
			"token_filters": []string{
				lowercase.Name,
			},
		},
	)
	if err != nil {
		return nil, err
	}

	err = indexMapping.AddCustomAnalyzer("fulltext",
		map[string]any{
			"type":      custom.Name,
			"tokenizer": unicode.Name,
			"token_filters": []string{
				lowercase.Name,
				porter.Name,
			},
		},
	)
	if err != nil {
		return nil, err
	}

	return indexMapping, nil
}

func searchResourceByID(id string, index bleve.Index) (*search.Resource, error) {
	req := bleve.NewSearchRequest(bleve.NewDocIDQuery([]string{id}))
	req.Fields = []string{"*"}
	res, err := index.Search(req)
	if err != nil {
		return nil, err
	}
	if res.Hits.Len() == 0 {
		return nil, errors.New("entity not found")
	}

	return matchToResource(res.Hits[0]), nil
}

func searchResourcesByPath(rootId, lookupPath string, index bleve.Index) ([]*search.Resource, error) {
	q := bleve.NewConjunctionQuery(
		bleve.NewQueryStringQuery("RootID:"+rootId),
		bleve.NewQueryStringQuery("Path:"+escapeQuery(lookupPath+"/*")),
	)
	bleveReq := bleve.NewSearchRequest(q)
	bleveReq.Size = math.MaxInt
	bleveReq.Fields = []string{"*"}
	res, err := index.Search(bleveReq)
	if err != nil {
		return nil, err
	}

	resources := make([]*search.Resource, 0, res.Hits.Len())
	for _, match := range res.Hits {
		resources = append(resources, matchToResource(match))
	}

	return resources, nil
}

func searchAndUpdateResourcesDeletionState(id string, state bool, index bleve.Index) ([]*search.Resource, error) {
	rootResource, err := searchResourceByID(id, index)
	if err != nil {
		return nil, err
	}
	rootResource.Deleted = state

	resources := []*search.Resource{rootResource}

	if rootResource.Type == uint64(storageProvider.ResourceType_RESOURCE_TYPE_CONTAINER) {
		descendantResources, err := searchResourcesByPath(rootResource.RootID, rootResource.Path, index)
		if err != nil {
			return nil, err
		}

		for _, descendantResource := range descendantResources {
			descendantResource.Deleted = state
			resources = append(resources, descendantResource)
		}
	}

	return resources, nil
}
