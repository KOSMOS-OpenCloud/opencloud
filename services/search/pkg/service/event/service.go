package event

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/search/pkg/metrics"
	"github.com/opencloud-eu/opencloud/services/search/pkg/search"
	"github.com/opencloud-eu/reva/v2/pkg/events"
	"github.com/opencloud-eu/reva/v2/pkg/events/raw"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var tracer trace.Tracer

func init() {
	tracer = otel.Tracer("github.com/opencloud-eu/opencloud/services/search/pkg/service/event")
}

// EnrichRequest represents a deferred Taki enrichment job.
type EnrichRequest struct {
	Ref      *provider.Reference
	Priority string // "high" (UI reindex), "normal" (upload), "low" (batch)
}

// Service defines the service handlers.
type Service struct {
	ctx                 context.Context
	log                 log.Logger
	tp                  trace.TracerProvider
	m                   *metrics.Metrics
	index               search.Searcher
	events              []events.Unmarshaller
	stream              raw.Stream
	indexSpaceDebouncer *SpaceDebouncer
	numConsumers        int
	purgeThreshold      int
	enrichCh            chan EnrichRequest
	enrichWorkers       int
	stopCh              chan struct{}
	stopped             *atomic.Bool
}

// New returns a service implementation for Service.
func New(ctx context.Context, stream raw.Stream, logger log.Logger, tp trace.TracerProvider, m *metrics.Metrics, index search.Searcher, debounceDuration int, numConsumers int, asyncUploads bool, purgeThreshold int, enrichWorkers int) (Service, error) {
	if enrichWorkers < 1 {
		enrichWorkers = 4
	}
	svc := Service{
		ctx:            ctx,
		log:            logger,
		tp:             tp,
		m:              m,
		index:          index,
		stream:         stream,
		purgeThreshold: purgeThreshold,
		enrichCh:       make(chan EnrichRequest, 1000),
		enrichWorkers:  enrichWorkers,
		stopCh:         make(chan struct{}, 1),
		stopped:        new(atomic.Bool),
		events: []events.Unmarshaller{
			events.ItemTrashed{},
			events.ItemPurged{},
			events.ItemRestored{},
			events.ItemMoved{},
			events.TrashbinPurged{},
			events.ContainerCreated{},
			events.FileTouched{},
			events.FileVersionRestored{},
			events.TagsAdded{},
			events.TagsRemoved{},
			events.ArbitraryMetadataUpdated{},
			events.SpaceRenamed{},
			events.LabelAdded{},
			events.LabelRemoved{},
		},
		numConsumers: numConsumers,
	}

	if asyncUploads {
		svc.events = append(svc.events, events.UploadReady{})
	} else {
		svc.events = append(svc.events, events.FileUploaded{})
	}

	svc.indexSpaceDebouncer = NewSpaceDebouncer(time.Duration(debounceDuration)*time.Millisecond, 30*time.Second, func(id *provider.StorageSpaceId) {
		if err := svc.index.IndexSpace(id, false); err != nil {
			svc.log.Error().Err(err).Interface("spaceID", id).Msg("error while indexing a space")
		}
	}, svc.log)

	return svc, nil
}

// Run to fulfil Runner interface
func (s Service) Run() error {
	ch, err := s.stream.Consume("search-pull", s.events...)
	if err != nil {
		return err
	}

	s.monitorAndPurge(s.ctx, "search-pull")

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	s.log.Debug().Int("worker.count", s.numConsumers).
		Str("messaging.consumer.group.name", "search-pull").
		Str("messaging.system", "nats").
		Str("messaging.operation.name", "receive").
		Msg("starting event processing workers")

	// start workers
	for i := 0; i < s.numConsumers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case e, ok := <-ch:
					if !ok {
						return
					}
					if err := s.processEvent(e); err != nil {
						s.log.Error().Err(err).
							Int("worker", workerID).
							Interface("event", e).
							Msg("failed to process event")
					}
				}
			}
		}(i)
	}

	// start enrich workers (Taki extraction, rate-limited)
	s.log.Info().Int("enrich_workers", s.enrichWorkers).Int("enrich_buffer", cap(s.enrichCh)).Msg("starting enrich worker pool")
	for i := 0; i < s.enrichWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case req, ok := <-s.enrichCh:
					if !ok {
						return
					}
					s.log.Info().
						Int("enrich_worker", workerID).
						Str("priority", req.Priority).
						Int("enrich_pending", len(s.enrichCh)).
						Msg("enrich: processing")
					s.index.UpsertItem(req.Ref)
				}
			}
		}(i)
	}

	// wait for stop signal
	<-s.stopCh
	cancel() // signal workers to stop
	wg.Wait()

	return nil
}

// Close will make the service to stop processing, so the `Run`
// method can finish.
// TODO: Underlying services can't be stopped. This means that some goroutines
// will get stuck trying to push events through a channel nobody is reading
// from, so resources won't be freed and there will be memory leaks. For now,
// if the service is stopped, you should close the app soon after.
func (s Service) Close() {
	if s.stopped.CompareAndSwap(false, true) {
		close(s.stopCh)
	}
}

// enqueueEnrich sends an enrichment request to the enrich worker pool.
// Non-blocking: drops the request if the queue is full (logs a warning).
func (s Service) enqueueEnrich(ref *provider.Reference, priority string) {
	select {
	case s.enrichCh <- EnrichRequest{Ref: ref, Priority: priority}:
		s.log.Info().
			Str("priority", priority).
			Int("enrich_pending", len(s.enrichCh)).
			Msg("enrich: queued")
	default:
		s.log.Warn().
			Str("priority", priority).
			Int("enrich_max", cap(s.enrichCh)).
			Msg("enrich: queue full, dropping request")
	}
}

func getSpaceID(ref *provider.Reference) *provider.StorageSpaceId {
	return &provider.StorageSpaceId{
		OpaqueId: storagespace.FormatResourceID(
			&provider.ResourceId{
				StorageId: ref.GetResourceId().GetStorageId(),
				SpaceId:   ref.GetResourceId().GetSpaceId(),
			},
		),
	}
}

func (s Service) processEvent(e raw.Event) error {
	ctx := e.GetTraceContext(s.ctx)
	_, span := tracer.Start(ctx, "processEvent")
	defer span.End()

	e.InProgress() // let nats know that we are processing this event
	s.log.Debug().Interface("event", e).Msg("updating index")

	switch ev := e.Event.Event.(type) {
	case events.ItemTrashed:
		s.index.TrashItem(ev.ID)
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.Ref), e.Ack)
	case events.ItemPurged:
		s.index.PurgeItem(ev.Ref)
		e.Ack()
	case events.TrashbinPurged:
		s.index.PurgeDeleted(getSpaceID(ev.Ref))
		e.Ack()
	case events.ItemMoved:
		s.index.MoveItem(ev.Ref)
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.Ref), e.Ack)
	case events.ItemRestored:
		s.index.RestoreItem(ev.Ref)
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.Ref), e.Ack)
	case events.ContainerCreated:
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.Ref), e.Ack)
	case events.FileTouched:
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.Ref), e.Ack)
	case events.FileVersionRestored:
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.Ref), e.Ack)
	case events.TagsAdded:
		// Tags: only Bleve update needed, no Taki
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.Ref), e.Ack)
	case events.TagsRemoved:
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.Ref), e.Ack)
	case events.ArbitraryMetadataUpdated:
		// Metadata changed: Bleve reindex (via debouncer) is enough
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.Ref), e.Ack)
	case events.FileUploaded:
		// Upload: fast-index (Bleve) + deferred enrich (Taki)
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.Ref), e.Ack)
		s.enqueueEnrich(ev.Ref, "normal")
	case events.UploadReady:
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.FileRef), e.Ack)
		s.enqueueEnrich(ev.FileRef, "normal")
	case events.SpaceRenamed:
		s.indexSpaceDebouncer.Debounce(ev.ID, e.Ack)
	case events.LabelAdded:
		// Favorites: only Bleve update needed, no Taki
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.Ref), e.Ack)
	case events.LabelRemoved:
		s.indexSpaceDebouncer.Debounce(getSpaceID(ev.Ref), e.Ack)
	}
	return nil
}

func (s Service) monitorAndPurge(ctx context.Context, name string) {
	consumer, err := s.stream.JetStream().Consumer(ctx, name)
	if err != nil {
		s.log.Error().Err(err).Msg("failed to get consumer")
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				info, err := consumer.Info(ctx)
				if err != nil {
					s.log.Error().Err(err).Msg("failed to get consumer info")
					continue
				}

				if s.m != nil {
					s.m.EventsOutstandingAcks.Set(float64(info.NumAckPending))
					s.m.EventsUnprocessed.Set(float64(info.NumPending))
					s.m.EventsRedelivered.Set(float64(info.NumRedelivered))
				}

				totalPending := int(info.NumPending) + info.NumAckPending
				if s.purgeThreshold > 0 && totalPending > s.purgeThreshold {
					s.log.Error().
						Int("pending", int(info.NumPending)).
						Int("ack_pending", info.NumAckPending).
						Int("total", totalPending).
						Int("threshold", s.purgeThreshold).
						Msg("SEARCH EVENT QUEUE OVERLOADED — purging stream. Run 'opencloud search index --all-spaces --force-rescan --insecure' to rebuild the index.")

					if err := s.stream.JetStream().Purge(ctx); err != nil {
						s.log.Error().Err(err).Msg("failed to purge stream")
					} else {
						s.log.Warn().
							Int("purged", totalPending).
							Msg("search event stream purged — index may be stale until reindex")
					}
				}
			}
		}
	}()
}
