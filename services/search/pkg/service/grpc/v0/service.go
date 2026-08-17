package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	user "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/jellydator/ttlcache/v2"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/token"
	"github.com/opencloud-eu/reva/v2/pkg/token/manager/jwt"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	merrors "go-micro.dev/v4/errors"
	"go-micro.dev/v4/metadata"
	grpcmetadata "google.golang.org/grpc/metadata"

	"github.com/opencloud-eu/opencloud/pkg/log"
	v0 "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/messages/search/v0"
	searchsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/search/v0"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config"
	"github.com/opencloud-eu/opencloud/services/search/pkg/search"
)

// NewHandler returns a service implementation for Service.
func NewHandler(opts ...Option) (searchsvc.SearchProviderHandler, error) {
	options := newOptions(opts...)
	cfg := options.Config
	if options.GatewaySelector == nil {
		return nil, errors.New("no GatewaySelector provided")
	}
	if options.Searcher == nil {
		return nil, errors.New("no Searcher provided")
	}

	cache := ttlcache.NewCache()
	if err := cache.SetTTL(time.Second); err != nil {
		return nil, err
	}

	tokenManager, err := jwt.New(map[string]any{
		"secret":  options.JWTSecret,
		"expires": int64(24 * 60 * 60),
	})
	if err != nil {
		return nil, err
	}

	return &Service{
		id:           cfg.GRPC.Namespace + "." + cfg.Service.Name,
		log:          &options.Logger,
		searcher:     options.Searcher,
		cache:        cache,
		tokenManager: tokenManager,
		gws:          options.GatewaySelector,
		cfg:          cfg,
	}, nil
}

// Service implements the searchServiceHandler interface
type Service struct {
	id           string
	log          *log.Logger
	searcher     search.Searcher
	cache        *ttlcache.Cache
	tokenManager token.Manager
	gws          *pool.Selector[gateway.GatewayAPIClient]
	cfg          *config.Config
}

// Search handles the search
func (s Service) Search(ctx context.Context, in *searchsvc.SearchRequest, out *searchsvc.SearchResponse) error {
	// Get token from the context (go-micro) and make it known to the reva client too (grpc)
	t, ok := metadata.Get(ctx, revactx.TokenHeader)
	if !ok {
		s.log.Error().Msg("Could not get token from context")
		return errors.New("could not get token from context")
	}
	ctx = grpcmetadata.AppendToOutgoingContext(ctx, revactx.TokenHeader, t)

	// unpack user
	u, _, err := s.tokenManager.DismantleToken(ctx, t)
	if err != nil {
		return err
	}
	ctx = revactx.ContextSetUser(ctx, u)

	key := cacheKey(in.Query, in.PageSize, in.Ref, u)
	res, ok := s.FromCache(key)
	if !ok {
		var err error
		res, err = s.searcher.Search(ctx, &searchsvc.SearchRequest{
			Query:    in.Query,
			PageSize: in.PageSize,
			Ref:      in.Ref,
		})
		if err != nil {
			switch err.(type) {
			case errtypes.BadRequest:
				return merrors.BadRequest(s.id, "%s", err.Error())
			default:
				return merrors.InternalServerError(s.id, "%s", err.Error())
			}
		}

		s.Cache(key, res)
	}

	out.Matches = res.Matches
	out.TotalMatches = res.TotalMatches
	out.NextPageToken = res.NextPageToken
	return nil
}

// IndexSpace (re)indexes all resources of a given space.
func (s Service) IndexSpace(_ context.Context, in *searchsvc.IndexSpaceRequest, _ *searchsvc.IndexSpaceResponse) error {
	// Workaround: go-micro server drops ReEnrich/ForceOverwrite bools when
	// called from standard gRPC client. Parse SpaceId prefix as signal.
	spaceID := in.GetSpaceId()
	reEnrich := in.GetReEnrich()
	forceOverwrite := in.GetForceOverwrite()
	if strings.HasPrefix(spaceID, "reenrich-overwrite:") {
		reEnrich = true
		forceOverwrite = true
		spaceID = strings.TrimPrefix(spaceID, "reenrich-overwrite:")
	} else if strings.HasPrefix(spaceID, "reenrich:") {
		reEnrich = true
		spaceID = strings.TrimPrefix(spaceID, "reenrich:")
	}

	s.log.Info().
		Str("space_id", spaceID).
		Bool("re_enrich", reEnrich).
		Bool("force_reindex", in.GetForceReindex()).
		Bool("force_overwrite", forceOverwrite).
		Msg("gRPC IndexSpace called")

	// Prüfen ob bereits ein Indexlauf aktiv ist
	if svc, ok := s.searcher.(*search.Service); ok {
		if status := svc.GetIndexStatus(); status.Running {
			s.log.Info().Str("space", spaceID).Msg("index already running, skipping")
			return nil
		}
	}

	if spaceID != "" {
		// Async: Job annehmen, sofort OK zurückgeben
		go func() {
			if svc, ok := s.searcher.(*search.Service); ok {
				svc.StartIndexing()
				svc.SetIndexProgress(1, 1)
				defer svc.FinishIndexing()
			}
			if reEnrich {
				if err := s.searcher.ReEnrichSpace(&provider.StorageSpaceId{OpaqueId: spaceID}, in.GetForceReindex(), forceOverwrite); err != nil {
					s.log.Error().Err(err).Str("space", spaceID).Msg("re-enrich space failed")
				}
			} else {
				if err := s.searcher.IndexSpace(&provider.StorageSpaceId{OpaqueId: spaceID}, in.GetForceReindex()); err != nil {
					s.log.Error().Err(err).Str("space", spaceID).Msg("index space failed")
				}
			}
		}()
		return nil
	}

	// index all spaces — ebenfalls async
	gwc, err := s.gws.Next()
	if err != nil {
		return err
	}

	ctx, err := utils.GetServiceUserContext(s.cfg.ServiceAccount.ServiceAccountID, gwc, s.cfg.ServiceAccount.ServiceAccountSecret)
	if err != nil {
		return err
	}

	resp, err := gwc.ListStorageSpaces(ctx, &provider.ListStorageSpacesRequest{})
	if err != nil {
		return err
	}

	if resp.GetStatus().GetCode() != rpc.Code_CODE_OK {
		return errors.New(resp.GetStatus().GetMessage())
	}

	spaces := resp.GetStorageSpaces()
	s.log.Info().Int("spaces", len(spaces)).Msg("index all spaces started")

	go func() {
		var indexErrors int
		if svc, ok := s.searcher.(*search.Service); ok {
			svc.StartIndexing()
			svc.SetIndexProgress(0, len(spaces))
			defer svc.FinishIndexing()
		}
		for i, space := range spaces {
			if svc, ok := s.searcher.(*search.Service); ok {
				svc.SetIndexProgress(i+1, len(spaces))
			}
			if reEnrich {
				if err := s.searcher.ReEnrichSpace(space.GetId(), in.GetForceReindex(), forceOverwrite); err != nil {
					s.log.Error().Err(err).Str("space", space.GetId().GetOpaqueId()).Msg("failed to re-enrich space, continuing")
					indexErrors++
				}
			} else {
				if err := s.searcher.IndexSpace(space.GetId(), in.GetForceReindex()); err != nil {
					s.log.Error().Err(err).Str("space", space.GetId().GetOpaqueId()).Msg("failed to index space, continuing")
					indexErrors++
				}
			}
		}
		if indexErrors > 0 {
			s.log.Warn().Int("errors", indexErrors).Msg("indexing completed with errors")
		} else {
			s.log.Info().Int("spaces", len(spaces)).Msg("indexing completed successfully")
		}
	}()

	return nil
}

// IndexItem (re-)indexes a single resource by its resource ID.
// The resource_id format is "storageid$spaceid!opaqueid".
// Fire-and-forget: antwortet sofort, Extraktion läuft im Hintergrund.
func (s Service) IndexItem(_ context.Context, in *searchsvc.IndexItemRequest, _ *searchsvc.IndexItemResponse) error {
	rid := in.ResourceId
	if rid == "" {
		return errors.New("resource_id is required")
	}

	parts := splitResourceID(rid)
	if parts == nil {
		return fmt.Errorf("invalid resource_id format: %s", rid)
	}

	ref := &provider.Reference{
		ResourceId: &provider.ResourceId{
			StorageId: parts[0],
			SpaceId:   parts[1],
			OpaqueId:  parts[2],
		},
		Path: ".",
	}

	s.log.Info().Str("resource_id", rid).Bool("force_overwrite", in.ForceOverwrite).Msg("IndexItem: enqueuing (priority=high)")
	s.searcher.EnqueueEnrich(ref, search.EnrichPriorityHigh, "grpc:IndexItem", in.ForceOverwrite)
	return nil
}

// Resolve resolves a (possibly outdated) resource ID to its current location via Bleve OldIDs index.
func (s Service) Resolve(_ context.Context, in *searchsvc.ResolveRequest, out *searchsvc.ResolveResponse) error {
	rid := in.ResourceId
	if rid == "" {
		return errors.New("resource_id is required")
	}

	resource, err := s.searcher.ResolvePathID(rid)
	if err != nil {
		out.Status = 1 // not found
		return nil
	}

	out.ResourceId = resource.ID
	out.Path = resource.Path
	out.RootId = resource.RootID
	out.Name = resource.Document.Name
	out.Status = 0
	return nil
}

// splitResourceID parses "storageid$spaceid!opaqueid" into [storageid, spaceid, opaqueid].
func splitResourceID(rid string) []string {
	dollarIdx := -1
	bangIdx := -1
	for i, c := range rid {
		if c == '$' && dollarIdx == -1 {
			dollarIdx = i
		}
		if c == '!' {
			bangIdx = i
		}
	}
	if dollarIdx == -1 || bangIdx == -1 || bangIdx <= dollarIdx {
		return nil
	}
	return []string{rid[:dollarIdx], rid[dollarIdx+1 : bangIdx], rid[bangIdx+1:]}
}

// FromCache pulls a search result from cache
func (s Service) FromCache(key string) (*searchsvc.SearchResponse, bool) {
	v, err := s.cache.Get(key)
	if err != nil {
		return nil, false
	}

	sr, ok := v.(*searchsvc.SearchResponse)
	return sr, ok
}

// Cache caches the search result
func (s Service) Cache(key string, res *searchsvc.SearchResponse) {
	// lets ignore the error
	_ = s.cache.Set(key, res)
}

func cacheKey(query string, pagesize int32, ref *v0.Reference, user *user.User) string {
	return fmt.Sprintf("%s|%d|%s$%s!%s/%s|%s", query, pagesize, ref.GetResourceId().GetStorageId(), ref.GetResourceId().GetSpaceId(), ref.GetResourceId().GetOpaqueId(), ref.GetPath(), user.GetId().GetOpaqueId())
}
