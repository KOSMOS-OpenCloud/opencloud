package search

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	rpcv1beta1 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	collaborationv1beta1 "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	libregraph "github.com/opencloud-eu/libre-graph-api-go"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	sdk "github.com/opencloud-eu/reva/v2/pkg/sdk/common"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/walker"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/tags"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/opencloud-eu/opencloud/pkg/log"
	searchmsg "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/messages/search/v0"
	searchsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config"
	"github.com/opencloud-eu/opencloud/services/search/pkg/content"
	"github.com/opencloud-eu/opencloud/services/search/pkg/metrics"
	"github.com/opencloud-eu/opencloud/services/search/pkg/qdrant"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	_spaceStateTrashed   = "trashed"
	_spaceTypeMountpoint = "mountpoint"
	_spaceTypePersonal   = "personal"
	_spaceTypeProject    = "project"
	_spaceTypeGrant      = "grant"
	_slowQueryDuration   = 500 * time.Millisecond
)

// EnrichPriority defines the priority of an enrich request.
const (
	EnrichPriorityHigh   = "high"   // UI reindex button — user waits
	EnrichPriorityNormal = "normal" // upload — background
	EnrichPriorityLow    = "low"    // batch re-enrich
)

// Searcher is the interface to the SearchService
type Searcher interface {
	Search(ctx context.Context, req *searchsvc.SearchRequest) (*searchsvc.SearchResponse, error)

	IndexSpace(rID *provider.StorageSpaceId, forceRescan bool) error
	ReEnrichSpace(rID *provider.StorageSpaceId, forceRescan, forceOverwrite bool) error
	PurgeDeleted(spaceID *provider.StorageSpaceId) error

	TrashItem(rID *provider.ResourceId)
	PurgeItem(rID *provider.Reference)
	UpsertItem(ref *provider.Reference)
	EnqueueIndex(ref *provider.Reference, source string)
	EnqueueEnrich(ref *provider.Reference, priority string, source string, forceOverwrite ...bool)
	RestoreItem(ref *provider.Reference)
	MoveItem(ref *provider.Reference)
}

// IndexStatus tracks the current indexing state for status queries.
type IndexStatus struct {
	Running        bool      `json:"running"`
	SpaceCurrent   int       `json:"space_current"`
	SpaceTotal     int       `json:"space_total"`
	SpaceID        string    `json:"space_id"`
	FilesProcessed int64     `json:"files_processed"`
	Errors         int       `json:"errors"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
}

// Service is responsible for indexing spaces and pass on a search
// to it's underlying engine.
type Service struct {
	logger          log.Logger
	gatewaySelector pool.Selectable[gateway.GatewayAPIClient]
	engine          Engine
	extractor       content.Extractor
	metrics         *metrics.Metrics
	cfg             *config.Config
	vectorClient    *qdrant.Client

	serviceAccountID     string
	serviceAccountSecret string

	batchSize      int
	indexStatus    IndexStatus
	indexMu        sync.Mutex
	upsertCounter  int64 // atomic op counter for doUpsertItem logging

	indexCh        chan queueRequest
	enrichCh       chan queueRequest
	enrichHighCh   chan queueRequest
	indexProcessed int64
	enrichProcessed int64
	indexFlushed   int64 // atomic: successful batch flushes
	indexBatchItems int64 // atomic: items currently in unflushed batch
	flushBlockedSince atomic.Pointer[time.Time] // set when flush starts, cleared when done
}

type queueRequest struct {
	ref            *provider.Reference
	priority       string // high, normal, low
	source         string // origin: "event:UploadReady", "event:FileUploaded", "grpc:IndexItem", "grpc:ReEnrich", etc.
	forceOverwrite bool   // overwrite existing metadata (xattrs) during enrichment
	op             string // "index" (default), "delete", "move"
	resourceID     string // for delete/move: formatted resource ID
	parentID       string // for move: formatted parent ID
	path           string // for move: new path
}

// GetIndexStatus returns the current indexing status.
func (s *Service) GetIndexStatus() IndexStatus {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	return s.indexStatus
}

// DocCount returns the number of documents in the index.
func (s *Service) DocCount() (uint64, error) {
	return s.engine.DocCount()
}

// StatsMap returns internal index statistics if the engine supports it.
type statsMapper interface {
	StatsMap() map[string]interface{}
}

func (s *Service) StatsMap() map[string]interface{} {
	if sm, ok := s.engine.(statsMapper); ok {
		return sm.StatsMap()
	}
	return map[string]interface{}{"error": "engine does not support StatsMap"}
}


// SetIndexProgress updates the space progress counters.
func (s *Service) SetIndexProgress(current, total int) {
	s.indexMu.Lock()
	s.indexStatus.SpaceCurrent = current
	s.indexStatus.SpaceTotal = total
	s.indexMu.Unlock()
}

var errSkipSpace error

// NewService creates a new Provider instance.
func NewService(gatewaySelector pool.Selectable[gateway.GatewayAPIClient], eng Engine, extractor content.Extractor, metrics *metrics.Metrics, logger log.Logger, cfg *config.Config) *Service {
	var s = &Service{
		gatewaySelector: gatewaySelector,
		engine:          eng,
		logger:          logger,
		extractor:       extractor,
		metrics:         metrics,
		cfg:             cfg,

		serviceAccountID:     cfg.ServiceAccount.ServiceAccountID,
		serviceAccountSecret: cfg.ServiceAccount.ServiceAccountSecret,

		batchSize: cfg.BatchSize,
		indexCh:      make(chan queueRequest, cfg.IndexQueueSize),
		enrichCh:     make(chan queueRequest, cfg.EnrichQueueSize),
		enrichHighCh: make(chan queueRequest, 100),
	}

	// Initialize Qdrant vector store if enabled
	if cfg.Vector.Enabled && cfg.Vector.URL != "" {
		s.vectorClient = qdrant.New(cfg.Vector.URL, cfg.Vector.Collection, logger)
		// Auto-create collection (768 dims = nomic-embed default)
		if err := s.vectorClient.EnsureCollection(768); err != nil {
			logger.Warn().Err(err).Msg("qdrant: collection init failed, vector search disabled")
			s.vectorClient = nil
		} else {
			logger.Info().
				Str("url", cfg.Vector.URL).
				Str("collection", cfg.Vector.Collection).
				Msg("vector search enabled (qdrant)")
		}
	}

	return s
}

// Search processes a search request and passes it down to the engine.
func (s *Service) Search(ctx context.Context, req *searchsvc.SearchRequest) (*searchsvc.SearchResponse, error) {
	searchStart := time.Now()
	defer func() {
		s.logger.Info().Str("query", req.Query).Str("total_duration", time.Since(searchStart).String()).Msg("search completed")
	}()
	s.logger.Debug().Str("query", req.Query).Msg("performing a search")

	// collect metrics
	startTime := time.Now()
	success := false
	defer func() {
		if s.metrics == nil {
			return
		}

		status := "success"
		if !success {
			status = "error"
		}
		s.metrics.SearchDuration.WithLabelValues(status).Observe(time.Since(startTime).Seconds())
	}()

	gatewayClient, err := s.gatewaySelector.Next()
	if err != nil {
		return nil, err
	}
	currentUser := revactx.ContextMustGetUser(ctx)

	// Handle flags
	query, flags := ParseFlags(req.Query)
	for _, flag := range flags {
		switch flag {
		case "is:favorite":
			query += " Favorites:\"" + currentUser.GetId().GetOpaqueId() + "\""
		}
	}

	// Extract content: term from query — route to Qdrant instead of bleve.
	query, contentTerm := extractContentQuery(query)

	// Extract scope from query if set
	query, scope := ParseScope(query)
	if query == "" {
		return nil, errtypes.BadRequest("empty query provided")
	}
	req.Query = query
	if len(scope) > 0 {
		scopedID, err := storagespace.ParseID(scope)
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to parse scope")
		}

		// Stat the scope to get the resource id
		statRes, err := gatewayClient.Stat(ctx, &provider.StatRequest{
			Ref: &provider.Reference{
				ResourceId: &scopedID,
			},
			FieldMask: &fieldmaskpb.FieldMask{Paths: []string{"space"}},
		})
		if err != nil {
			return nil, err
		}
		// GetPath the scope to get the full path in the space
		gpRes, err := gatewayClient.GetPath(ctx, &provider.GetPathRequest{
			ResourceId: statRes.GetInfo().GetId(),
		})
		if err != nil {
			return nil, err
		}

		req.Ref = &searchmsg.Reference{
			ResourceId: &searchmsg.ResourceID{
				StorageId: statRes.GetInfo().GetSpace().GetRoot().GetStorageId(),
				SpaceId:   statRes.GetInfo().GetSpace().GetRoot().GetSpaceId(),
				OpaqueId:  statRes.GetInfo().GetSpace().GetRoot().GetOpaqueId(),
			},
			Path: gpRes.Path,
		}
	}
	filters := []*provider.ListStorageSpacesRequest_Filter{
		{
			Type: provider.ListStorageSpacesRequest_Filter_TYPE_USER,
			Term: &provider.ListStorageSpacesRequest_Filter_User{User: currentUser.GetId()},
		},
		{
			Type: provider.ListStorageSpacesRequest_Filter_TYPE_SPACE_TYPE,
			Term: &provider.ListStorageSpacesRequest_Filter_SpaceType{SpaceType: "+grant"},
		},
	}

	// Get the spaces to search
	listSpacesStart := time.Now()
	spaces := []*provider.StorageSpace{}
	listSpacesRes, err := gatewayClient.ListStorageSpaces(ctx, &provider.ListStorageSpacesRequest{Filters: filters})
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list the user's storage spaces")
		return nil, err
	}
	for _, space := range listSpacesRes.StorageSpaces {
		if utils.ReadPlainFromOpaque(space.Opaque, "trashed") == _spaceStateTrashed {
			// Do not consider disabled spaces
			continue
		}
		if space.SpaceType != "mountpoint" && req.Ref != nil && (req.Ref.GetResourceId().GetSpaceId() != space.Root.GetSpaceId()) {
			// Do not search (non-mountpoint) spaces that do not match the given scope (if a scope is set)
			// We still need the mountpoint in order to map the result paths to the according share
			continue
		}
		spaces = append(spaces, space)
	}

	mountpointMap := map[string]string{}
	for _, space := range spaces {
		if space.SpaceType != _spaceTypeMountpoint {
			continue
		}
		opaqueMap := sdk.DecodeOpaqueMap(space.Opaque)
		grantSpaceID := storagespace.FormatResourceID(&provider.ResourceId{
			StorageId: opaqueMap["grantStorageID"],
			SpaceId:   opaqueMap["grantSpaceID"],
			OpaqueId:  opaqueMap["grantOpaqueID"],
		})
		mountpointMap[grantSpaceID] = space.Id.OpaqueId
	}

	s.logger.Info().Str("list_spaces_duration", time.Since(listSpacesStart).String()).Int("spaces", len(spaces)).Msg("search: spaces listed")

	matches := matchArray{}
	total := int32(0)

	bleveStart := time.Now()
	errg, ctx := errgroup.WithContext(ctx)
	work := make(chan *provider.StorageSpace, len(spaces))
	results := make(chan *searchsvc.SearchIndexResponse, len(spaces))

	// Distribute work
	errg.Go(func() error {
		defer close(work)
		for _, space := range spaces {
			select {
			case work <- space:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	// Spawn workers that'll concurrently work the queue
	numWorkers := 20
	if len(spaces) < numWorkers {
		numWorkers = len(spaces)
	}
	for i := 0; i < numWorkers; i++ {
		errg.Go(func() error {
			for space := range work {
				res, err := s.searchIndex(ctx, req, space, mountpointMap[space.Id.OpaqueId])
				if err != nil && err != errSkipSpace {
					return err
				}

				select {
				case results <- res:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
	}

	// Wait for things to settle down, then close results chan
	go func() {
		_ = errg.Wait() // error is checked later
		close(results)
	}()

	responses := make([]*searchsvc.SearchIndexResponse, len(spaces))
	i := 0
	for r := range results {
		responses[i] = r
		i++
	}

	if err := errg.Wait(); err != nil {
		return nil, err
	}

	for _, res := range responses {
		if res == nil {
			continue
		}
		total += res.TotalMatches
		for _, match := range res.Matches {
			matches = append(matches, match)
		}
	}

	s.logger.Info().Str("bleve_duration", time.Since(bleveStart).String()).Int("matches", len(matches)).Msg("search: bleve done")

	// Qdrant semantic search: when content: term is present or freetext query
	qdrantStart := time.Now()
	qdrantQuery := req.Query
	if contentTerm != "" {
		qdrantQuery = contentTerm // use extracted content term for embedding
	}
	if s.vectorClient != nil && (contentTerm != "" || isFreetext(req.Query)) {
		contentReq := *req
		contentReq.Query = qdrantQuery
		vectorMatches := s.searchVector(ctx, &contentReq, gatewayClient, spaces, mountpointMap)
		if len(vectorMatches) > 0 {
			// Merge: add vector results that aren't already in keyword results
			existingIDs := map[string]bool{}
			for _, m := range matches {
				existingIDs[m.Entity.Id.OpaqueId] = true
			}
			for _, vm := range vectorMatches {
				if !existingIDs[vm.Entity.Id.OpaqueId] {
					matches = append(matches, vm)
					total++
				}
			}
		}
	}

	// compile one sorted list of matches from all spaces and apply the limit if needed
	sort.Sort(matches)
	limit := req.PageSize
	if limit == 0 {
		limit = 200
	}
	if int32(len(matches)) > limit && limit != -1 {
		matches = matches[0:limit]
	}

	s.logger.Info().Str("qdrant_duration", time.Since(qdrantStart).String()).Int("total_matches", len(matches)).Msg("search: qdrant done")

	success = true
	return &searchsvc.SearchResponse{
		Matches:      matches,
		TotalMatches: total,
	}, nil
}

// isFreetext returns true if the query is plain text without field prefixes.
func isFreetext(query string) bool {
	// Structured queries contain field:value patterns
	for _, prefix := range []string{"name:", "tag:", "mtime", "mediatype:", "Type:", "id:", "RootID:", "Path:", "Favorites:"} {
		if strings.Contains(query, prefix) {
			return false
		}
	}
	return true
}

// searchVector performs semantic search via Qdrant and returns matching resources.
func (s *Service) searchVector(ctx context.Context, req *searchsvc.SearchRequest, gatewayClient gateway.GatewayAPIClient, spaces []*provider.StorageSpace, mountpointMap map[string]string) []*searchmsg.Match {
	// Get embedding for query from open_taki
	embedding := s.getQueryEmbedding(req.Query)
	if embedding == nil {
		return nil
	}

	results, err := s.vectorClient.Search(embedding, 20)
	if err != nil {
		s.logger.Warn().Err(err).Msg("qdrant search failed")
		return nil
	}

	// Build set of accessible space IDs for permission filtering
	allowedSpaces := make(map[string]bool, len(spaces))
	for _, sp := range spaces {
		allowedSpaces[sp.Id.OpaqueId] = true
	}

	// Convert Qdrant results to search matches
	var matches []*searchmsg.Match
	for _, result := range results {
		threshold := s.cfg.Vector.ScoreThreshold
		if threshold <= 0 {
			threshold = 0.6 // default
		}
		if result.Score < threshold {
			continue
		}

		// Build match from Qdrant payload — no Stat call needed
		p := result.Payload
		ridStr, _ := p["resource_id"].(string)
		if ridStr == "" {
			continue
		}
		resourceID, err := storagespace.ParseID(ridStr)
		if err != nil {
			continue
		}

		// Filter by space permissions — skip results from spaces the user cannot access
		if !allowedSpaces[resourceID.SpaceId] {
			continue
		}

		name, _ := p["name"].(string)
		mime, _ := p["mime"].(string)
		path, _ := p["path"].(string)
		var size uint64
		if s, ok := p["size"].(float64); ok {
			size = uint64(s)
		}

		match := &searchmsg.Match{
			Score: float32(result.Score),
			Entity: &searchmsg.Entity{
				Ref: &searchmsg.Reference{
					ResourceId: &searchmsg.ResourceID{
						StorageId: resourceID.StorageId,
						SpaceId:   resourceID.SpaceId,
						OpaqueId:  resourceID.OpaqueId,
					},
					Path: path,
				},
				Id: &searchmsg.ResourceID{
					StorageId: resourceID.StorageId,
					SpaceId:   resourceID.SpaceId,
					OpaqueId:  resourceID.OpaqueId,
				},
				Name:     name,
				Size:     size,
				MimeType: mime,
			},
		}

		if mtime, ok := p["mtime"].(float64); ok && mtime > 0 {
			match.Entity.LastModifiedTime = &timestamppb.Timestamp{
				Seconds: int64(mtime),
			}
		}

		matches = append(matches, match)
	}

	s.logger.Debug().
		Str("query", req.Query).
		Int("qdrant_hits", len(results)).
		Int("valid_matches", len(matches)).
		Msg("vector search completed")

	return matches
}

// getQueryEmbedding gets an embedding for the search query via the taki extractor.
func (s *Service) getQueryEmbedding(query string) []float64 {
	tikaExtractor, ok := s.extractor.(*content.Tika)
	if !ok || !tikaExtractor.IsTaki() {
		return nil
	}
	return tikaExtractor.GetEmbedding(query)
}

func (s *Service) searchIndex(ctx context.Context, req *searchsvc.SearchRequest, space *provider.StorageSpace, mountpointID string) (*searchsvc.SearchIndexResponse, error) {
	if req.Ref != nil &&
		(req.Ref.ResourceId.StorageId != space.Root.StorageId ||
			req.Ref.ResourceId.SpaceId != space.Root.SpaceId) {
		return nil, errSkipSpace
	}

	searchRootID := &searchmsg.ResourceID{
		StorageId: space.Root.StorageId,
		SpaceId:   space.Root.SpaceId,
		OpaqueId:  space.Root.OpaqueId,
	}

	var (
		mountpointRootID *searchmsg.ResourceID
		rootName         string
		permissions      *provider.ResourcePermissions
		remoteItemId     *searchmsg.ResourceID
	)
	mountpointPrefix := ""
	searchPathPrefix := req.Ref.GetPath()
	switch space.SpaceType {
	case _spaceTypeMountpoint:
		return nil, errSkipSpace // mountpoint spaces are only "links" to the shared spaces. we have to search the shared "grant" space instead
	case _spaceTypeGrant:
		// In case of grant spaces we search the root of the outer space and translate the paths to the according mountpoint
		searchRootID.OpaqueId = space.Root.SpaceId
		if mountpointID == "" {
			s.logger.Warn().Interface("space", space).Msg("could not find mountpoint space for grant space")
			return nil, errSkipSpace
		}

		gatewayClient, err := s.gatewaySelector.Next()
		if err != nil {
			return nil, err
		}

		serviceCtx, err := getAuthContext(s.serviceAccountID, s.gatewaySelector, s.serviceAccountSecret, s.logger)
		if err != nil {
			return nil, err
		}

		gpRes, err := gatewayClient.GetPath(serviceCtx, &provider.GetPathRequest{
			ResourceId: space.Root,
		})
		if err != nil {
			s.logger.Error().Err(err).Str("space", space.Id.OpaqueId).Msg("failed to get path for grant space root")
			return nil, errSkipSpace
		}
		if gpRes.Status.Code != rpcv1beta1.Code_CODE_OK {
			s.logger.Error().Interface("status", gpRes.Status).Str("space", space.Id.OpaqueId).Msg("failed to get path for grant space root")
			return nil, errSkipSpace
		}
		mountpointPrefix = utils.MakeRelativePath(gpRes.Path)
		if searchPathPrefix == "" {
			searchPathPrefix = mountpointPrefix
		}
		sid, spid, oid, err := storagespace.SplitID(mountpointID)
		if err != nil {
			s.logger.Error().Err(err).Str("space", space.Id.OpaqueId).Str("mountpointId", mountpointID).Msg("invalid mountpoint space id")
			return nil, errSkipSpace
		}
		// exclude the hidden shares
		rs, err := gatewayClient.GetReceivedShare(ctx, &collaborationv1beta1.GetReceivedShareRequest{
			Ref: &collaborationv1beta1.ShareReference{
				Spec: &collaborationv1beta1.ShareReference_Id{
					Id: &collaborationv1beta1.ShareId{
						OpaqueId: oid,
					},
				},
			},
		})
		if err != nil {
			s.logger.Error().Err(err).Str("space", space.Id.OpaqueId).Str("shareId", oid).Msg("invalid receive share")
		}
		if rs.GetStatus().GetCode() == rpcv1beta1.Code_CODE_OK && rs.GetShare().GetHidden() {
			return nil, errSkipSpace
		}

		mountpointRootID = &searchmsg.ResourceID{
			StorageId: sid,
			SpaceId:   spid,
			OpaqueId:  oid,
		}
		rootName = space.GetRootInfo().GetPath()
		permissions = space.GetRootInfo().GetPermissionSet()
		remoteItemId = &searchmsg.ResourceID{
			StorageId: space.GetRootInfo().GetId().GetStorageId(),
			SpaceId:   space.GetRootInfo().GetId().GetSpaceId(),
			OpaqueId:  space.GetRootInfo().GetId().GetOpaqueId(),
		}
		s.logger.Debug().Interface("grantSpace", space).Interface("mountpointRootId", mountpointRootID).Msg("searching a grant")
	case _spaceTypePersonal, _spaceTypeProject:
		permissions = space.GetRootInfo().GetPermissionSet()
	}

	searchRequest := &searchsvc.SearchIndexRequest{
		Query: req.Query,
		Ref: &searchmsg.Reference{
			ResourceId: searchRootID,
			Path:       searchPathPrefix,
		},
		PageSize: req.PageSize,
	}
	start := time.Now()
	res, err := s.engine.Search(ctx, searchRequest)
	duration := time.Since(start)
	if err != nil {
		s.logger.Error().Err(err).Str("duration", fmt.Sprint(duration)).Str("space", space.Id.OpaqueId).Msg("failed to search the index")
		return nil, err
	}
	if duration > _slowQueryDuration {
		s.logger.Info().Interface("searchRequest", searchRequest).Str("duration", fmt.Sprint(duration)).Str("space", space.Id.OpaqueId).Int("hits", len(res.Matches)).Msg("slow space search")
	} else {
		s.logger.Debug().Interface("searchRequest", searchRequest).Str("duration", fmt.Sprint(duration)).Str("space", space.Id.OpaqueId).Int("hits", len(res.Matches)).Msg("space search done")
	}

	matches := make([]*searchmsg.Match, 0, len(res.Matches))

	for _, match := range res.Matches {
		if mountpointPrefix != "" {
			match.Entity.Ref.Path = utils.MakeRelativePath(strings.TrimPrefix(match.Entity.Ref.Path, mountpointPrefix))
		}
		if mountpointRootID != nil {
			match.Entity.Ref.ResourceId = mountpointRootID
		}
		match.Entity.ShareRootName = rootName
		match.Entity.RemoteItemId = remoteItemId

		isShared := match.GetEntity().GetRef().GetResourceId().GetSpaceId() == utils.ShareStorageSpaceID
		isMountpoint := isShared && match.GetEntity().GetRef().GetPath() == "."
		isDir := match.GetEntity().GetMimeType() == "httpd/unix-directory"
		match.Entity.Permissions = convertToWebDAVPermissions(isShared, isMountpoint, isDir, permissions)

		if req.Ref != nil && searchPathPrefix == "/"+match.Entity.Name {
			continue
		}

		matches = append(matches, match)
	}

	res.Matches = matches

	return res, nil
}

// StartIndexing marks the index job as running. Call FinishIndexing when done.
func (s *Service) StartIndexing() {
	s.indexMu.Lock()
	s.indexStatus.Running = true
	s.indexStatus.FilesProcessed = 0
	s.indexStatus.Errors = 0
	s.indexStatus.StartedAt = time.Now()
	s.indexMu.Unlock()
}

// FinishIndexing marks the index job as finished.
func (s *Service) FinishIndexing() {
	s.indexMu.Lock()
	s.indexStatus.Running = false
	s.indexStatus.FinishedAt = time.Now()
	s.indexMu.Unlock()
}

// IndexSpace (re)indexes all resources of a given space.
func (s *Service) IndexSpace(spaceID *provider.StorageSpaceId, forceRescan bool) error {
	s.indexMu.Lock()
	s.indexStatus.SpaceID = spaceID.GetOpaqueId()
	s.indexMu.Unlock()
	s.logger.Info().Str("space", spaceID.GetOpaqueId()).Bool("force", forceRescan).Msg("IndexSpace starting")
	ownerCtx, err := getAuthContext(s.serviceAccountID, s.gatewaySelector, s.serviceAccountSecret, s.logger)
	if err != nil {
		return err
	}

	rootID, err := storagespace.ParseID(spaceID.OpaqueId)
	if err != nil {
		s.logger.Error().Err(err).Str("space_id", spaceID.OpaqueId).Msg("invalid space id")
		return err
	}
	if rootID.StorageId == "" || rootID.SpaceId == "" {
		s.logger.Error().Str("space_id", spaceID.OpaqueId).Msg("invalid space id: missing StorageId or SpaceId")
		return fmt.Errorf("invalid space id: %s", spaceID.OpaqueId)
	}
	rootID.OpaqueId = rootID.SpaceId

	// Collect metrics
	startTime := time.Now()
	success := false
	defer func() {
		if s.metrics == nil {
			return
		}
		status := "success"
		if !success {
			status = "error"
		}
		s.metrics.IndexDuration.WithLabelValues(status).Observe(time.Since(startTime).Seconds())
	}()

	w := walker.NewWalker(s.gatewaySelector)

	err = w.Walk(ownerCtx, &rootID, func(wd string, info *provider.ResourceInfo, err error) error {
		if err != nil {
			s.logger.Error().Err(err).Msg("error walking the tree")
			return err
		}

		if info == nil {
			return nil
		}

		ref := &provider.Reference{
			Path:       utils.MakeRelativePath(filepath.Join(wd, info.Path)),
			ResourceId: &rootID,
		}
		atomic.AddInt64(&s.indexStatus.FilesProcessed, 1)

		if !forceRescan {
			// Skip unchanged files. Never skip directories — always walk into them
			// to catch children that might be missing from the index.
			if info.Type != provider.ResourceType_RESOURCE_TYPE_CONTAINER {
				searchRes, err := s.engine.Search(ownerCtx, &searchsvc.SearchIndexRequest{
					Query: "id:" + storagespace.FormatResourceID(info.Id) + ` mtime>=` + utils.TSToTime(info.Mtime).Format(time.RFC3339Nano),
				})
				if err == nil && len(searchRes.Matches) >= 1 {
					return nil // file unchanged, skip
				}
			}
		}

		// Fast index via queue: metadata only from ResourceInfo (no Taki/LLM)
		s.EnqueueIndex(ref, "walk:IndexSpace")
		return nil
	})

	if err != nil {
		return err
	}

	s.logger.Info().
		Str("space", spaceID.GetOpaqueId()).
		Int64("files", atomic.LoadInt64(&s.indexStatus.FilesProcessed)).
		Int("errors", s.indexStatus.Errors).
		Str("duration", time.Since(startTime).String()).
		Msg("IndexSpace completed")

	success = true
	return nil
}

// ReindexPath walks a specific path within a space and reindexes all items.
// Returns detailed debug info about each item: found/skipped/indexed/errors.
// spaceID format: "storageid$spaceid" (e.g. "f7e671d7...$5ac86946...")
// path: relative path within the space (e.g. "Innere Verwaltung/Kopierer")
func (s *Service) ReindexPath(spaceID, path string) ([]map[string]interface{}, error) {
	ownerCtx, err := getAuthContext(s.serviceAccountID, s.gatewaySelector, s.serviceAccountSecret, s.logger)
	if err != nil {
		return nil, fmt.Errorf("auth failed: %w", err)
	}

	rootID, err := storagespace.ParseID(spaceID)
	if err != nil {
		return nil, fmt.Errorf("invalid space id %q: %w", spaceID, err)
	}
	if rootID.StorageId == "" || rootID.SpaceId == "" {
		return nil, fmt.Errorf("invalid space id (missing storage/space): %s", spaceID)
	}
	rootID.OpaqueId = rootID.SpaceId

	// Step 1: Stat the target path
	gwc, err := s.gatewaySelector.Next()
	if err != nil {
		return nil, fmt.Errorf("gateway error: %w", err)
	}

	targetRef := &provider.Reference{
		ResourceId: &rootID,
		Path:       utils.MakeRelativePath(path),
	}
	statResp, err := gwc.Stat(ownerCtx, &provider.StatRequest{Ref: targetRef})
	if err != nil {
		return nil, fmt.Errorf("stat failed: %w", err)
	}
	if statResp.Status.Code != rpc.Code_CODE_OK {
		return nil, fmt.Errorf("stat error: %s (%s)", statResp.Status.Message, statResp.Status.Code)
	}

	var results []map[string]interface{}

	targetInfo := statResp.Info
	results = append(results, map[string]interface{}{
		"action": "stat",
		"path":   path,
		"name":   targetInfo.Name,
		"type":   targetInfo.Type.String(),
		"id":     storagespace.FormatResourceID(targetInfo.Id),
		"status": "ok",
	})

	// Step 2: If directory, ListContainer to get children
	if targetInfo.Type != provider.ResourceType_RESOURCE_TYPE_CONTAINER {
		// Single file — just index it
		ref := &provider.Reference{
			Path:       utils.MakeRelativePath(path),
			ResourceId: &rootID,
		}
		s.EnqueueIndex(ref, "debug:ReindexPath")
		results = append(results, map[string]interface{}{
			"action": "index",
			"name":   targetInfo.Name,
			"status": "queued",
		})
		return results, nil
	}

	// ListContainer on the directory
	listResp, err := gwc.ListContainer(ownerCtx, &provider.ListContainerRequest{
		Ref:                   targetRef,
		ArbitraryMetadataKeys: []string{"*"},
	})
	if err != nil {
		return nil, fmt.Errorf("ListContainer failed: %w", err)
	}
	if listResp.Status.Code != rpc.Code_CODE_OK {
		return nil, fmt.Errorf("ListContainer error: %s (%s)", listResp.Status.Message, listResp.Status.Code)
	}

	results = append(results, map[string]interface{}{
		"action":   "list",
		"path":     path,
		"children": len(listResp.Infos),
		"status":   "ok",
	})

	// Step 3: Index each child via queue
	for _, info := range listResp.Infos {
		childPath := filepath.Join(path, info.Name)
		ref := &provider.Reference{
			Path:       utils.MakeRelativePath(childPath),
			ResourceId: &rootID,
		}

		entry := map[string]interface{}{
			"action":   "index",
			"name":     info.Name,
			"path":     childPath,
			"id":       storagespace.FormatResourceID(info.Id),
			"type":     info.Type.String(),
			"size":     info.Size,
			"mimeType": info.MimeType,
		}

		// Check if already in index
		searchRes, sErr := s.engine.Search(ownerCtx, &searchsvc.SearchIndexRequest{
			Query: "id:" + storagespace.FormatResourceID(info.Id),
		})
		if sErr == nil && len(searchRes.Matches) > 0 {
			entry["in_index"] = true
		} else {
			entry["in_index"] = false
		}

		s.EnqueueIndex(ref, "debug:ReindexPath")
		entry["status"] = "queued"

		// Check arbitrary metadata
		if info.ArbitraryMetadata != nil && len(info.ArbitraryMetadata.Metadata) > 0 {
			entry["metadata_keys"] = len(info.ArbitraryMetadata.Metadata)
		}

		results = append(results, entry)
	}

	results = append(results, map[string]interface{}{
		"action": "batch_queued",
		"status": "ok",
	})

	return results, nil
}

// TrashItem marks the item as deleted via the index-queue.
func (s *Service) TrashItem(rID *provider.ResourceId) {
	select {
	case s.indexCh <- queueRequest{op: "delete", resourceID: storagespace.FormatResourceID(rID), source: "event:TrashItem"}:
	default:
		s.logger.Warn().Str("id", storagespace.FormatResourceID(rID)).Msg("index-queue full, dropping delete")
	}
}

func (s *Service) PurgeItem(ref *provider.Reference) {
	if ref.Path != "" && ref.Path != "." {
		s.logger.Warn().Str("path", ref.Path).Msg("purging an item with a path is not supported")
		return
	}

	select {
	case s.indexCh <- queueRequest{op: "purge", resourceID: storagespace.FormatResourceID(ref.ResourceId), source: "event:PurgeItem"}:
	default:
		s.logger.Warn().Interface("Id", ref.ResourceId).Msg("index-queue full, dropping purge")
	}
}

func (s *Service) PurgeDeleted(spaceID *provider.StorageSpaceId) error {
	if spaceID == nil {
		return fmt.Errorf("spaceID must not be nil")
	}

	rootID, err := storagespace.ParseID(spaceID.OpaqueId)
	if err != nil {
		s.logger.Error().Err(err).Str("space_id", spaceID.OpaqueId).Msg("invalid space id")
		return err
	}
	if rootID.StorageId == "" || rootID.SpaceId == "" {
		s.logger.Error().Str("space_id", spaceID.OpaqueId).Msg("invalid space id: missing StorageId or SpaceId")
		return fmt.Errorf("invalid space id: %s", spaceID.OpaqueId)
	}
	rootID.OpaqueId = rootID.SpaceId

	select {
	case s.indexCh <- queueRequest{op: "purge-deleted", resourceID: storagespace.FormatResourceID(&rootID), source: "event:PurgeDeleted"}:
	default:
		s.logger.Warn().Interface("Id", &rootID).Msg("index-queue full, dropping purge-deleted")
	}
	return nil
}

// ReEnrichSpace walks all files in a space, calls Taki/LLM for each,
// and writes metadata to xattrs. By default only missing keys are written.
// If force is true, all keys are overwritten (destructive — user corrections lost).
// Bleve is updated automatically via ArbitraryMetadataUpdated events.
func (s *Service) ReEnrichSpace(spaceID *provider.StorageSpaceId, forceRescan, forceOverwrite bool) error {
	s.logger.Info().Str("space", spaceID.GetOpaqueId()).Bool("forceRescan", forceRescan).Bool("forceOverwrite", forceOverwrite).Msg("ReEnrichSpace starting")
	ownerCtx, err := getAuthContext(s.serviceAccountID, s.gatewaySelector, s.serviceAccountSecret, s.logger)
	if err != nil {
		return err
	}

	rootID, err := storagespace.ParseID(spaceID.OpaqueId)
	if err != nil {
		return fmt.Errorf("invalid space id: %w", err)
	}
	if rootID.StorageId == "" || rootID.SpaceId == "" {
		return fmt.Errorf("invalid space id")
	}
	rootID.OpaqueId = rootID.SpaceId

	startTime := time.Now()
	w := walker.NewWalker(s.gatewaySelector)

	// Parallel Taki extraction
	indexWorkers := s.cfg.Extractor.Tika.MaxWorkers
	if indexWorkers < 1 {
		indexWorkers = 8
	}

	var enriched, skipped, errors int64
	workCh := make(chan *provider.Reference, indexWorkers*2)
	var wg sync.WaitGroup

	for i := 0; i < indexWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ref := range workCh {
				if s.doEnrichItem(ownerCtx, ref, forceRescan, forceOverwrite) {
					atomic.AddInt64(&enriched, 1)
				} else {
					atomic.AddInt64(&skipped, 1)
				}
			}
		}()
	}

	s.logger.Info().
		Str("space", spaceID.GetOpaqueId()).
		Bool("forceRescan", forceRescan).
		Bool("forceOverwrite", forceOverwrite).
		Int("workers", indexWorkers).
		Msg("re-enrich: starting")

	err = w.Walk(ownerCtx, &rootID, func(wd string, info *provider.ResourceInfo, err error) error {
		if err != nil {
			atomic.AddInt64(&errors, 1)
			return err
		}
		if info == nil || info.Type == provider.ResourceType_RESOURCE_TYPE_CONTAINER {
			return nil
		}
		ref := &provider.Reference{
			Path:       utils.MakeRelativePath(filepath.Join(wd, info.Path)),
			ResourceId: &rootID,
		}
		workCh <- ref
		return nil
	})

	close(workCh)
	wg.Wait()

	s.logger.Info().
		Str("space", spaceID.GetOpaqueId()).
		Int64("enriched", enriched).
		Int64("skipped", skipped).
		Int64("errors", errors).
		Str("duration", time.Since(startTime).String()).
		Msg("re-enrich: completed")

	return err
}

// doEnrichItem extracts metadata via Taki and writes missing (or all if forceOverwrite) xattrs.
// Returns true if metadata was written, false if skipped.
// When forceRescan is false, items that already have doc.type are skipped entirely
// (no Taki call). Use forceRescan to re-extract everything.
// forceOverwrite controls whether existing metadata keys are overwritten.
func (s *Service) doEnrichItem(ctx context.Context, ref *provider.Reference, forceRescan, forceOverwrite bool) bool {
	_, stat, path := s.resInfo(ref)
	if stat == nil || path == "" {
		return false
	}

	// Skip already-enriched items (doc.type present) unless forceRescan
	if !forceRescan {
		existing := stat.GetInfo().GetArbitraryMetadata().GetMetadata()
		if existing != nil {
			if dt, ok := existing["doc.type"]; ok && dt != "" {
				return false
			}
		}
	}

	// Attach trace ID for Taki debug correlation
	traceID := fmt.Sprintf("enrich_%s", storagespace.FormatResourceID(stat.Info.Id))
	ctx = content.ContextWithTraceID(ctx, traceID)

	doc, err := s.extractor.Extract(ctx, stat.Info)
	if err != nil {
		s.logger.Error().Err(err).Str("path", path).Msg("re-enrich: extraction failed")
		return false
	}

	// Collect metadata from extraction
	metadata := map[string]string{}
	addAudioMetadata(metadata, doc.Audio)
	addImageMetadata(metadata, doc.Image)
	addLocationMetadata(metadata, doc.Location)
	addPhotoMetadata(metadata, doc.Photo)
	if doc.Taki != nil {
		addDocMetadata(metadata, doc.Taki.DocMeta)
	}
	if len(metadata) == 0 {
		return false
	}

	// Filter: only missing keys unless forceOverwrite
	writeMetadata := metadata
	if !forceOverwrite {
		existing := stat.GetInfo().GetArbitraryMetadata().GetMetadata()
		writeMetadata = map[string]string{}
		for k, v := range metadata {
			if v == "" {
				continue
			}
			if existing != nil {
				if _, exists := existing[k]; exists {
					continue
				}
			}
			writeMetadata[k] = v
		}
	}
	if len(writeMetadata) == 0 {
		return false
	}

	// Write xattrs via SetArbitraryMetadata → triggers ArbitraryMetadataUpdated → Bleve auto-update
	gatewayClient, err := s.gatewaySelector.Next()
	if err != nil {
		s.logger.Error().Err(err).Msg("re-enrich: no gateway client")
		return false
	}

	resp, err := gatewayClient.SetArbitraryMetadata(ctx, &provider.SetArbitraryMetadataRequest{
		Ref: ref,
		ArbitraryMetadata: &provider.ArbitraryMetadata{
			Metadata: writeMetadata,
		},
	})
	if err != nil || resp.Status.Code != rpc.Code_CODE_OK {
		s.logger.Error().Err(err).Str("path", path).Msg("re-enrich: SetArbitraryMetadata failed")
		return false
	}

	s.logger.Info().
		Str("name", doc.Name).
		Int("keys", len(writeMetadata)).
		Bool("forceOverwrite", forceOverwrite).
		Msg("re-enrich: metadata written")

	// Also update Qdrant embedding if available
	hasTaki := doc.Taki != nil
	hasEmbed := hasTaki && len(doc.Taki.Embed) > 0
	s.logger.Info().
		Str("name", doc.Name).
		Str("trace", traceID).
		Bool("has_taki", hasTaki).
		Bool("has_embed", hasEmbed).
		Bool("has_vector_client", s.vectorClient != nil).
		Int("content_len", len(doc.Content)).
		Msg("re-enrich: backend routing")
	if s.vectorClient != nil && hasEmbed {
		payload := map[string]interface{}{
			"name":        doc.Name,
			"title":       doc.Title,
			"mime":        doc.MimeType,
			"size":        stat.Info.Size,
			"mtime":       stat.Info.Mtime.Seconds,
			"method":      doc.Taki.Method,
			"path":        utils.MakeRelativePath(path),
			"resource_id": storagespace.FormatResourceID(stat.Info.Id),
		}
		if doc.Taki.Summary != "" {
			payload["summary"] = doc.Taki.Summary
		}
		point := qdrant.Point{
			ID:      stat.Info.Id.OpaqueId,
			Vector:  doc.Taki.Embed,
			Payload: payload,
		}
		if err := s.vectorClient.Upsert([]qdrant.Point{point}); err != nil {
			s.logger.Warn().Err(err).Str("name", doc.Name).Msg("re-enrich: qdrant upsert failed")
		}
	}

	return true
}

// UpsertItem indexes or stores Resource data fields (legacy, calls both queues).
func (s *Service) UpsertItem(ref *provider.Reference) {
	s.EnqueueIndex(ref, "legacy:UpsertItem")
	s.EnqueueEnrich(ref, EnrichPriorityNormal, "legacy:UpsertItem")
}

// EnqueueIndex sends a Bleve-only index request for a single item (metadata, no Taki).
func (s *Service) EnqueueIndex(ref *provider.Reference, source string) {
	select {
	case s.indexCh <- queueRequest{ref: ref, source: source}:
		s.logger.Debug().Str("source", source).Int("index_pending", len(s.indexCh)).Msg("index-queue: queued")
	default:
		s.logger.Warn().Str("source", source).Int("index_max", cap(s.indexCh)).Msg("index-queue: full, dropping")
	}
}

// EnqueueEnrich sends a Taki enrichment request (Taki + Qdrant + xattrs + Bleve with content).
func (s *Service) EnqueueEnrich(ref *provider.Reference, priority string, source string, forceOverwrite ...bool) {
	overwrite := len(forceOverwrite) > 0 && forceOverwrite[0]
	req := queueRequest{ref: ref, priority: priority, source: source, forceOverwrite: overwrite}
	if priority == EnrichPriorityHigh {
		select {
		case s.enrichHighCh <- req:
			s.logger.Info().
				Str("source", source).
				Int("enrich_high_pending", len(s.enrichHighCh)).
				Msg("enrich-queue: queued (high priority)")
		default:
			s.logger.Warn().
				Str("source", source).
				Int("enrich_high_max", cap(s.enrichHighCh)).
				Msg("enrich-queue-high: full, dropping")
		}
		return
	}
	select {
	case s.enrichCh <- req:
		s.logger.Info().
			Str("source", source).
			Str("priority", priority).
			Int("enrich_pending", len(s.enrichCh)).
			Msg("enrich-queue: queued")
	default:
		s.logger.Warn().
			Str("source", source).
			Str("priority", priority).
			Int("enrich_max", cap(s.enrichCh)).
			Msg("enrich-queue: full, dropping")
	}
}

// QueueStats returns current queue statistics.
type QueueStats struct {
	Pending      int    `json:"pending"`
	Max          int    `json:"max"`
	Processed    int64  `json:"processed"`
	Flushed      int64  `json:"flushed,omitempty"`
	BatchItems   int64  `json:"batch_items,omitempty"`
	FlushBlocked string `json:"flush_blocked_since,omitempty"`
}

func (s *Service) IndexQueueStats() QueueStats {
	qs := QueueStats{
		Pending:    len(s.indexCh),
		Max:        cap(s.indexCh),
		Processed:  atomic.LoadInt64(&s.indexProcessed),
		Flushed:    atomic.LoadInt64(&s.indexFlushed),
		BatchItems: atomic.LoadInt64(&s.indexBatchItems),
	}
	if t := s.flushBlockedSince.Load(); t != nil {
		qs.FlushBlocked = t.Format("2006-01-02T15:04:05Z07:00")
	}
	return qs
}

func (s *Service) EnrichQueueStats() QueueStats {
	return QueueStats{
		Pending:   len(s.enrichCh) + len(s.enrichHighCh),
		Max:       cap(s.enrichCh),
		Processed: atomic.LoadInt64(&s.enrichProcessed),
	}
}

// StartWorkers starts the index and enrich worker goroutines. Call once.
func (s *Service) StartWorkers(ctx context.Context) {
	// Index Worker: Bleve-only updates with batching
	// Collects items for up to 500ms or batchSize items, then flushes as one Bleve batch.
	s.logger.Info().Int("batch_size", s.batchSize).Msg("starting index-queue worker (batched)")
	go func() {
		batch, err := s.engine.NewBatch(s.batchSize)
		if err != nil {
			s.logger.Error().Err(err).Msg("index-queue: failed to create batch")
			return
		}
		batchCount := 0
		flushTimer := time.NewTimer(500 * time.Millisecond)
		flushTimer.Stop()

		flush := func() {
			if batchCount == 0 {
				return
			}
			atomic.StoreInt64(&s.indexBatchItems, int64(batchCount))
			now := time.Now()
			s.flushBlockedSince.Store(&now)
			if err := batch.Push(); err != nil {
				s.logger.Error().Err(err).Int("items", batchCount).Msg("index-queue: batch flush failed")
			} else {
				atomic.AddInt64(&s.indexFlushed, 1)
				s.logger.Info().Int("items", batchCount).Msg("index-queue: batch flushed")
			}
			s.flushBlockedSince.Store(nil)
			atomic.StoreInt64(&s.indexBatchItems, 0)
			batchCount = 0
			flushTimer.Stop()
		}

		// waitForMerger pauses writes if too many segments have accumulated,
		// giving the merger time to consolidate before we push more.
		// If the merger goroutine is dead (async errors fired, merge epoch
		// stuck) we log an alarm and stop waiting — blocking forever would
		// be worse than proceeding with degraded merge.
		var mergerAlarmFired bool
		waitForMerger := func() {
			const maxSegments = 200
			const checkInterval = 2 * time.Second
			const maxWait = 60 * time.Second
			start := time.Now()
			for {
				stats := s.StatsMap()

				// --- segment count check ---
				segs, ok := stats["num_root_filesegments"]
				if !ok {
					return // can't check, proceed
				}
				n, ok := segs.(uint64)
				if !ok {
					return
				}
				if n < maxSegments {
					return
				}

				// --- merger-dead detection ---
				// If async errors have been fired AND the merge epoch is
				// behind the root epoch, the merger goroutine is dead.
				asyncErrors, _ := stats["TotOnErrors"].(uint64)
				rootEpoch, _ := stats["CurRootEpoch"].(uint64)
				lastMerged, _ := stats["LastMergedEpoch"].(uint64)
				mergerStuck := asyncErrors > 0 && rootEpoch > 0 && lastMerged < rootEpoch

				if mergerStuck && !mergerAlarmFired {
					mergerAlarmFired = true
					s.logger.Error().
						Uint64("segments", n).
						Uint64("async_errors", asyncErrors).
						Uint64("root_epoch", rootEpoch).
						Uint64("last_merged_epoch", lastMerged).
						Msg("*** SEARCH ALARM: merger goroutine appears dead — " +
							"segments will not be merged. Index is degraded. " +
							"A process restart is required. ***")
				}

				// --- timeout ---
				if time.Since(start) > maxWait {
					s.logger.Error().
						Uint64("segments", n).
						Bool("merger_stuck", mergerStuck).
						Dur("waited", time.Since(start)).
						Msg("index-queue: giving up waiting for merger after timeout — " +
							"proceeding with writes despite high segment count")
					return
				}

				s.logger.Warn().
					Uint64("segments", n).
					Bool("merger_stuck", mergerStuck).
					Msg("index-queue: waiting for merger to consolidate segments")
				time.Sleep(checkInterval)
			}
		}

		for {
			select {
			case <-ctx.Done():
				flush()
				return
			case <-flushTimer.C:
				flush()
				waitForMerger()
			case req, ok := <-s.indexCh:
				if !ok {
					flush()
					return
				}
				switch req.op {
				case "delete":
					flush()
					if err := s.engine.Delete(req.resourceID); err != nil {
						s.logger.Error().Err(err).Str("id", req.resourceID).Msg("index-queue: delete failed")
					}
				case "move":
					flush()
					if err := s.engine.Move(req.resourceID, req.parentID, req.path); err != nil {
						s.logger.Error().Err(err).Str("path", req.path).Msg("index-queue: move failed")
					}
				case "purge":
					flush()
					if err := s.engine.Purge(req.resourceID, false); err != nil {
						s.logger.Error().Err(err).Str("id", req.resourceID).Msg("index-queue: purge failed")
					}
				case "purge-deleted":
					flush()
					if err := s.engine.Purge(req.resourceID, true); err != nil {
						s.logger.Error().Err(err).Str("id", req.resourceID).Msg("index-queue: purge-deleted failed")
					}
				case "restore":
					flush()
					if err := s.engine.Restore(req.resourceID); err != nil {
						s.logger.Error().Err(err).Str("id", req.resourceID).Msg("index-queue: restore failed")
					}
				default: // "index" or empty
					s.doIndexItemBatch(req.ref, batch)
					batchCount++
				}
				atomic.AddInt64(&s.indexProcessed, 1)

				if batchCount >= s.batchSize {
					flush()
					waitForMerger()
				} else if batchCount == 1 {
					flushTimer.Reset(500 * time.Millisecond)
				}
			}
		}
	}()

	// Enrich Worker: Taki extraction + Qdrant + xattrs + Bleve (full content)
	// High-priority items (UI reindex) are processed before normal items.
	s.logger.Info().Msg("starting enrich-queue worker")
	go func() {
		for {
			// Drain high-priority channel first
			select {
			case <-ctx.Done():
				return
			case req, ok := <-s.enrichHighCh:
				if !ok {
					return
				}
				s.logger.Info().
					Str("source", req.source).
					Bool("overwrite", req.forceOverwrite).
					Int("enrich_high_pending", len(s.enrichHighCh)).
					Msg("enrich-queue: processing (high priority)")
				s.doUpsertItem(req.ref, nil, req.forceOverwrite)
				atomic.AddInt64(&s.enrichProcessed, 1)
				continue
			default:
			}
			// No high-priority items — process normal
			select {
			case <-ctx.Done():
				return
			case req, ok := <-s.enrichHighCh:
				if !ok {
					return
				}
				s.logger.Info().
					Str("source", req.source).
					Bool("overwrite", req.forceOverwrite).
					Int("enrich_high_pending", len(s.enrichHighCh)).
					Msg("enrich-queue: processing (high priority)")
				s.doUpsertItem(req.ref, nil, req.forceOverwrite)
				atomic.AddInt64(&s.enrichProcessed, 1)
			case req, ok := <-s.enrichCh:
				if !ok {
					return
				}
				s.logger.Info().
					Str("source", req.source).
					Str("priority", req.priority).
					Int("enrich_pending", len(s.enrichCh)).
					Msg("enrich-queue: processing")
				s.doUpsertItem(req.ref, nil)
				atomic.AddInt64(&s.enrichProcessed, 1)
			}
		}
	}()
}

// doUpsertItem indexes or stores Resource data fields.
// doFastIndex indexes a resource from its ResourceInfo without calling Taki.
// Used for quick index rebuilds — only metadata, no content extraction.
func (s *Service) doFastIndex(info *provider.ResourceInfo, ref *provider.Reference, batch BatchOperator) {
	id := storagespace.FormatResourceID(info.Id)
	rootID := storagespace.FormatResourceID(&provider.ResourceId{
		StorageId: info.Id.StorageId,
		OpaqueId:  info.Id.SpaceId,
		SpaceId:   info.Id.SpaceId,
	})

	mtime := ""
	if info.Mtime != nil {
		mtime = utils.TSToTime(info.Mtime).Format(time.RFC3339Nano)
	}

	r := Resource{
		ID:     id,
		RootID: rootID,
		Path:   ref.Path,
		Type:   uint64(info.Type),
	}
	r.Name = info.Name
	r.Title = info.Name
	r.Size = info.Size
	r.MimeType = info.MimeType
	r.Mtime = mtime
	// Copy arbitrary metadata (xattrs: oy.fileReference, oy.subject, etc.)
	if info.ArbitraryMetadata != nil && len(info.ArbitraryMetadata.Metadata) > 0 {
		r.Metadata = info.ArbitraryMetadata.Metadata
	}
	r.Hidden = strings.HasPrefix(r.Path, ".")
	if parentID := info.GetParentId(); parentID != nil {
		r.ParentID = storagespace.FormatResourceID(parentID)
	}
	// Copy tags from ArbitraryMetadata
	if m := info.ArbitraryMetadata.GetMetadata(); m != nil {
		if t, ok := m["tags"]; ok {
			r.Tags = tags.New(t).AsSlice()
		}
	}
	// Copy favorites from Opaque (list of user IDs who favorited this item)
	if m := info.Opaque.GetMap(); m != nil && m["favorites"] != nil {
		if favEntry := m["favorites"]; favEntry.Decoder == "json" {
			var favorites []string
			if err := json.Unmarshal(favEntry.Value, &favorites); err == nil {
				r.Favorites = favorites
			}
		}
	}

	var err error
	if batch != nil {
		err = batch.Upsert(id, r)
	} else {
		// No batch — route through index-queue for batched writes
		s.EnqueueIndex(ref, "fastIndex:noBatch")
		return
	}
	if err != nil {
		s.logger.Error().Err(err).Str("path", ref.Path).Msg("fast index: error upserting")
		s.indexMu.Lock()
		s.indexStatus.Errors++
		s.indexMu.Unlock()
	}
}

// doIndexItemBatch adds a Bleve-only update to a batch (metadata from Stat, no Taki call).
// The batch is flushed by the index worker when full or after a timeout.
func (s *Service) doIndexItemBatch(ref *provider.Reference, batch BatchOperator) {
	ctx, stat, path := s.resInfo(ref)
	if ctx == nil || stat == nil || path == "" {
		return
	}

	r := Resource{
		ID: storagespace.FormatResourceID(stat.Info.Id),
		RootID: storagespace.FormatResourceID(&provider.ResourceId{
			StorageId: stat.Info.Id.StorageId,
			OpaqueId:  stat.Info.Id.SpaceId,
			SpaceId:   stat.Info.Id.SpaceId,
		}),
		Path: utils.MakeRelativePath(path),
		Type: uint64(stat.Info.Type),
	}

	// basic.Extract: metadata, tags, favorites — no Taki
	doc, err := content.NewBasicExtractor(s.logger)
	if err != nil {
		s.logger.Error().Err(err).Str("name", stat.Info.Name).Msg("doIndexItem: basic extractor failed")
		return
	}
	r.Document, err = doc.Extract(ctx, stat.Info)
	if err != nil {
		s.logger.Error().Err(err).Str("name", stat.Info.Name).Msg("doIndexItem: extract failed")
		return
	}
	r.Name = stat.Info.Name
	r.Hidden = strings.HasPrefix(r.Path, ".")
	if parentID := stat.GetInfo().GetParentId(); parentID != nil {
		r.ParentID = storagespace.FormatResourceID(parentID)
	}

	if err := batch.Upsert(r.ID, r); err != nil {
		s.logger.Error().Err(err).Str("name", stat.Info.Name).Msg("doIndexItem: batch upsert failed")
	}
}

func (s *Service) doUpsertItem(ref *provider.Reference, batch BatchOperator, forceOverwrite ...bool) {
	overwrite := len(forceOverwrite) > 0 && forceOverwrite[0]
	opID := atomic.AddInt64(&s.upsertCounter, 1)
	t0 := time.Now()

	refID := ""
	if ref.GetResourceId() != nil {
		refID = storagespace.FormatResourceID(ref.GetResourceId())
	}
	s.logger.Info().Int64("op", opID).Str("ref", refID).Str("path", ref.GetPath()).Msg("doUpsertItem: start")

	ctx, stat, path := s.resInfo(ref)
	if ctx == nil || stat == nil || path == "" {
		s.logger.Warn().Int64("op", opID).Str("ref", refID).Dur("stat_ms", time.Since(t0)).Msg("doUpsertItem: resInfo failed (stat returned nil)")
		return
	}
	tStat := time.Since(t0)

	s.logger.Info().Int64("op", opID).Str("name", stat.Info.Name).Str("path", path).Str("mime", stat.Info.MimeType).Dur("stat_ms", tStat).Msg("doUpsertItem: stat ok, extracting")

	// Attach trace ID for Taki debug correlation
	traceID := fmt.Sprintf("op%d", opID)
	ctx = content.ContextWithTraceID(ctx, traceID)

	tExtract := time.Now()
	doc, err := s.extractor.Extract(ctx, stat.Info)
	if err != nil {
		s.logger.Error().Err(err).Int64("op", opID).Str("path", path).Dur("extract_ms", time.Since(tExtract)).Msg("doUpsertItem: extract failed")
		s.indexMu.Lock()
		s.indexStatus.Errors++
		s.indexMu.Unlock()
		return
	}

	s.logger.Info().
		Int64("op", opID).
		Str("name", doc.Name).
		Int("content_len", len(doc.Content)).
		Int("tags", len(doc.Tags)).
		Int("favorites", len(doc.Favorites)).
		Int("metadata", len(doc.Metadata)).
		Bool("has_taki", doc.Taki != nil).
		Dur("extract_ms", time.Since(tExtract)).
		Msg("doUpsertItem: extract ok")

	r := Resource{
		ID: storagespace.FormatResourceID(stat.Info.Id),
		RootID: storagespace.FormatResourceID(&provider.ResourceId{
			StorageId: stat.Info.Id.StorageId,
			OpaqueId:  stat.Info.Id.SpaceId,
			SpaceId:   stat.Info.Id.SpaceId,
		}),
		Path:     utils.MakeRelativePath(path),
		Type:     uint64(stat.Info.Type),
		Document: doc,
	}
	r.Hidden = strings.HasPrefix(r.Path, ".")

	if parentID := stat.GetInfo().GetParentId(); parentID != nil {
		r.ParentID = storagespace.FormatResourceID(parentID)
	}

	// Taki v2 routing: decide what goes to bleve based on content_target
	bleveDoc := doc
	if doc.Taki != nil && doc.Taki.Routing != nil {
		target := doc.Taki.Routing.ContentTarget
		if target == "vector" || target == "none" {
			// Content should NOT go to bleve — clear it for the bleve index
			// but keep Title (meta always goes to bleve)
			bleveDoc.Content = ""
			s.logger.Debug().
				Str("name", doc.Name).
				Str("target", target).
				Str("method", doc.Taki.Method).
				Msg("taki routing: content excluded from bleve index")
		}
	}

	bleveResource := r
	bleveResource.Document = bleveDoc

	if batch != nil {
		s.logger.Info().Int64("op", opID).Str("name", doc.Name).Msg("doUpsertItem: bleve upsert starting (batch)")
		tBleve := time.Now()
		err = batch.Upsert(r.ID, bleveResource)
		if err != nil {
			s.logger.Error().Err(err).Int64("op", opID).Str("name", doc.Name).Dur("bleve_ms", time.Since(tBleve)).Msg("doUpsertItem: bleve upsert failed")
		} else {
			s.logger.Info().Int64("op", opID).Str("name", doc.Name).Dur("bleve_ms", time.Since(tBleve)).Msg("doUpsertItem: bleve upsert ok")
		}
	} else {
		// Route through index-queue for batched writes (avoids 1-segment-per-write)
		s.EnqueueIndex(ref, "enrich:doUpsertItem")
	}

	// Taki v2: log + store embedding in Qdrant
	if doc.Taki != nil {
		s.logger.Info().
			Int64("op", opID).
			Str("name", doc.Name).
			Str("method", doc.Taki.Method).
			Int("content_len", len(doc.Content)).
			Int("embedding_dims", len(doc.Taki.Embed)).
			Int("entities", len(doc.Taki.Entities)).
			Int("metadata", len(doc.Metadata)).
			Str("summary", doc.Taki.Summary).
			Msg("doUpsertItem: taki v2 complete")

		// Store embedding in Qdrant if enabled and embedding present
		if s.vectorClient != nil && len(doc.Taki.Embed) > 0 {
			payload := map[string]interface{}{
				"name":     doc.Name,
				"title":    doc.Title,
				"mime":     doc.MimeType,
				"size":     stat.Info.Size,
				"mtime":    stat.Info.Mtime.Seconds,
				"method":   doc.Taki.Method,
				"path":     r.Path,
				"root_id":  r.RootID,
			}
			if doc.Taki.Summary != "" {
				payload["summary"] = doc.Taki.Summary
			}
			if len(doc.Taki.Entities) > 0 {
				payload["entities"] = doc.Taki.Entities
			}
			// Truncate content for payload (full text is in bleve)
			if len(doc.Content) > 2000 {
				payload["content_preview"] = doc.Content[:2000]
			} else if doc.Content != "" {
				payload["content_preview"] = doc.Content
			}

			payload["resource_id"] = r.ID // full OpenCloud ID (storageId$spaceId!opaqueId)
			point := qdrant.Point{
				ID:      stat.Info.Id.OpaqueId, // pure UUID, stable across moves
				Vector:  doc.Taki.Embed,
				Payload: payload,
			}

			tQdrant := time.Now()
			if err := s.vectorClient.Upsert([]qdrant.Point{point}); err != nil {
				s.logger.Error().Err(err).Int64("op", opID).Str("name", doc.Name).Dur("qdrant_ms", time.Since(tQdrant)).Msg("doUpsertItem: qdrant upsert failed")
			} else {
				s.logger.Info().Int64("op", opID).Str("name", doc.Name).Int("dims", len(doc.Taki.Embed)).Dur("qdrant_ms", time.Since(tQdrant)).Msg("doUpsertItem: qdrant upsert ok")
			}
		}
	}

	// determine if metadata needs to be stored in storage as well
	metadata := map[string]string{}
	addAudioMetadata(metadata, doc.Audio)
	addImageMetadata(metadata, doc.Image)
	addLocationMetadata(metadata, doc.Location)
	addPhotoMetadata(metadata, doc.Photo)
	if doc.Taki != nil {
		addDocMetadata(metadata, doc.Taki.DocMeta)
	}
	if len(metadata) == 0 {
		return
	}

	// Only write metadata keys that don't already exist on the resource,
	// unless forceOverwrite is set (UI reindex with overwrite option).
	existing := stat.GetInfo().GetArbitraryMetadata().GetMetadata()
	newMetadata := map[string]string{}
	for k, v := range metadata {
		if v == "" {
			continue // never write empty values
		}
		if !overwrite && existing != nil {
			if _, exists := existing[k]; exists {
				continue // don't overwrite existing xattr
			}
		}
		newMetadata[k] = v
	}
	if len(newMetadata) == 0 {
		return
	}

	s.logger.Info().Int64("op", opID).Str("name", doc.Name).Int("keys", len(newMetadata)).Bool("overwrite", overwrite).Interface("metadata", newMetadata).Msg("doUpsertItem: writing metadata xattrs")

	gatewayClient, err := s.gatewaySelector.Next()
	if err != nil {
		s.logger.Error().Err(err).Msg("could not retrieve client to store metadata")
		return
	}

	resp, err := gatewayClient.SetArbitraryMetadata(ctx, &provider.SetArbitraryMetadataRequest{
		Ref: ref,
		ArbitraryMetadata: &provider.ArbitraryMetadata{
			Metadata: newMetadata,
		},
	})
	if err != nil || resp.GetStatus().GetCode() != rpc.Code_CODE_OK {
		s.logger.Error().Err(err).Int64("op", opID).Int32("status", int32(resp.GetStatus().GetCode())).Str("name", doc.Name).Msg("doUpsertItem: metadata write failed")
		return
	}
	s.logger.Info().Int64("op", opID).Str("name", doc.Name).Int("keys", len(newMetadata)).Dur("total_ms", time.Since(t0)).Msg("doUpsertItem: done")
}

func addAudioMetadata(metadata map[string]string, audio *libregraph.Audio) {
	if audio == nil {
		return
	}
	marshalToStringMap(audio, metadata, "libre.graph.audio.")
}

func addImageMetadata(metadata map[string]string, image *libregraph.Image) {
	if image == nil {
		return
	}
	marshalToStringMap(image, metadata, "libre.graph.image.")
}

func addLocationMetadata(metadata map[string]string, location *libregraph.GeoCoordinates) {
	if location == nil {
		return
	}
	marshalToStringMap(location, metadata, "libre.graph.location.")
}

func addPhotoMetadata(metadata map[string]string, photo *libregraph.Photo) {
	if photo == nil {
		return
	}
	marshalToStringMap(photo, metadata, "libre.graph.photo.")
}

// addDocMetadata dynamically flattens structured document metadata from open_taki
// into the metadata map. Walks all sub-objects (doc.*, sender.*, recipient.*, amounts.*).
// Stored as xattr: user.oc.md.doc.subject, user.oc.md.sender.company, etc.
// New fields added to docmeta_schema.json flow through without code changes.
func addDocMetadata(metadata map[string]string, dm *content.TakiDocMeta) {
	if dm == nil {
		return
	}
	m := map[string]interface{}(*dm)
	for key, val := range m {
		sub, ok := val.(map[string]interface{})
		if !ok {
			continue
		}
		for sk, sv := range sub {
			fmt.Printf("search-debug: %s.%s = %v (%T)\n", key, sk, sv, sv)
		}
	}

	// Flatten all sub-objects dynamically
	for key, val := range m {
		sub, ok := val.(map[string]interface{})
		if !ok {
			continue
		}
		for subKey, subVal := range sub {
			if subVal == nil {
				continue
			}
			if strVal, ok := subVal.(string); ok && strVal != "" {
				metadata[key+"."+subKey] = strVal
			} else if boolVal, ok := subVal.(bool); ok {
				if boolVal {
					metadata[key+"."+subKey] = "true"
				}
			}
		}
	}

	// source metadata
	if source, ok := m["source"].(string); ok && source != "" {
		metadata["doc.meta_source"] = source
	}
}

func marshalToStringMap[T libregraph.MappedNullable](source T, target map[string]string, prefix string) {
	// ToMap never returns a non-nil error ...
	m, _ := source.ToMap()

	for k, v := range m {
		if v == nil {
			continue
		}
		target[prefix+k] = valueToString(v)
	}
}

func valueToString(value any) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case *string:
		return *v
	case *int32:
		return strconv.FormatInt(int64(*v), 10)
	case *int64:
		return strconv.FormatInt(*v, 10)
	case *float32:
		return strconv.FormatFloat(float64(*v), 'f', -1, 32)
	case *float64:
		return strconv.FormatFloat(*v, 'f', -1, 64)
	case *bool:
		return strconv.FormatBool(*v)
	case *time.Time:
		return v.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// RestoreItem makes the item available again.
func (s *Service) RestoreItem(ref *provider.Reference) {
	ctx, stat, path := s.resInfo(ref)
	if ctx == nil || stat == nil || path == "" {
		return
	}

	select {
	case s.indexCh <- queueRequest{op: "restore", resourceID: storagespace.FormatResourceID(stat.Info.Id), source: "event:RestoreItem"}:
	default:
		s.logger.Warn().Str("id", storagespace.FormatResourceID(stat.Info.Id)).Msg("index-queue full, dropping restore")
	}
}

// MoveItem updates the resource location and all of its necessary fields.
func (s *Service) MoveItem(ref *provider.Reference) {
	ctx, stat, path := s.resInfo(ref)
	if ctx == nil || stat == nil || path == "" {
		return
	}

	select {
	case s.indexCh <- queueRequest{
		op:         "move",
		resourceID: storagespace.FormatResourceID(stat.GetInfo().GetId()),
		parentID:   storagespace.FormatResourceID(stat.GetInfo().GetParentId()),
		path:       path,
		source:     "event:MoveItem",
	}:
	default:
		s.logger.Warn().Str("path", path).Msg("index-queue full, dropping move")
	}
}

func (s *Service) resInfo(ref *provider.Reference) (context.Context, *provider.StatResponse, string) {
	ownerCtx, err := getAuthContext(s.serviceAccountID, s.gatewaySelector, s.serviceAccountSecret, s.logger)
	if err != nil {
		return nil, nil, ""
	}

	statRes, err := statResource(ownerCtx, ref, s.gatewaySelector, s.logger)
	if err != nil {
		return nil, nil, ""
	}

	r, err := ResolveReference(ownerCtx, ref, statRes.GetInfo(), s.gatewaySelector)
	if err != nil {
		return nil, nil, ""
	}

	return ownerCtx, statRes, r.GetPath()
}

// DebugSearch searches the bleve index directly without auth context.
// Returns raw results for debugging.
func (s *Service) DebugSearch(query string, limit int) (interface{}, error) {
	ctx := context.Background()
	req := &searchsvc.SearchIndexRequest{
		Query:    query,
		PageSize: int32(limit),
	}
	res, err := s.engine.Search(ctx, req)
	if err != nil {
		return nil, err
	}

	type debugMatch struct {
		ID        string   `json:"id"`
		RootID    string   `json:"root_id"`
		Name      string   `json:"name"`
		Path      string   `json:"path"`
		Type      string   `json:"type"`
		Favorites []string `json:"favorites,omitempty"`
		Score     float64  `json:"score"`
	}
	matches := []debugMatch{}
	for _, m := range res.Matches {
		e := m.GetEntity()
		dm := debugMatch{
			Name:  e.GetName(),
			Score: float64(m.GetScore()),
		}
		if id := e.GetId(); id != nil {
			dm.ID = fmt.Sprintf("%s$%s!%s", id.GetStorageId(), id.GetSpaceId(), id.GetOpaqueId())
		}
		if pid := e.GetParentId(); pid != nil {
			dm.RootID = fmt.Sprintf("%s$%s", pid.GetStorageId(), pid.GetSpaceId())
		}
		if ref := e.GetRef(); ref != nil {
			dm.Path = ref.GetPath()
		}
		matches = append(matches, dm)
	}
	docCount, _ := s.engine.DocCount()
	return map[string]interface{}{
		"query":    query,
		"total":    res.TotalMatches,
		"doccount": docCount,
		"matches":  matches,
	}, nil
}
