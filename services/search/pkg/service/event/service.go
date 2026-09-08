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

// Service defines the service handlers.
type Service struct {
	ctx                 context.Context
	log                 log.Logger
	tp                  trace.TracerProvider
	m                   *metrics.Metrics
	index               search.Searcher
	events              []events.Unmarshaller
	stream              raw.Stream
	numConsumers        int
	purgeThreshold      int
	stopCh              chan struct{}
	stopped             *atomic.Bool
}

// New returns a service implementation for Service.
func New(ctx context.Context, stream raw.Stream, logger log.Logger, tp trace.TracerProvider, m *metrics.Metrics, index search.Searcher, numConsumers int, asyncUploads bool, purgeThreshold int) (Service, error) {
	svc := Service{
		ctx:            ctx,
		log:            logger,
		tp:             tp,
		m:              m,
		index:          index,
		stream:         stream,
		purgeThreshold: purgeThreshold,
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
		e.Ack()
	case events.ItemPurged:
		s.index.PurgeItem(ev.Ref)
		e.Ack()
	case events.TrashbinPurged:
		s.index.PurgeDeleted(getSpaceID(ev.Ref))
		e.Ack()
	case events.ItemMoved:
		s.index.MoveItem(ev.Ref)
		e.Ack()
	case events.ItemRestored:
		s.index.RestoreItem(ev.Ref)
		s.index.EnqueueIndex(ev.Ref, "event:ItemRestored")
		e.Ack()
	case events.ContainerCreated:
		s.index.EnqueueIndex(ev.Ref, "event:ContainerCreated")
		e.Ack()
	case events.FileTouched:
		s.index.EnqueueIndex(ev.Ref, "event:FileTouched")
		e.Ack()
	case events.FileVersionRestored:
		s.index.EnqueueIndex(ev.Ref, "event:FileVersionRestored")
		e.Ack()
	case events.TagsAdded:
		s.index.EnqueueIndex(ev.Ref, "event:TagsAdded")
		e.Ack()
	case events.TagsRemoved:
		s.index.EnqueueIndex(ev.Ref, "event:TagsRemoved")
		e.Ack()
	case events.ArbitraryMetadataUpdated:
		s.index.EnqueueIndex(ev.Ref, "event:ArbitraryMetadataUpdated")
		e.Ack()
	case events.FileUploaded:
		s.index.EnqueueIndex(ev.Ref, "event:FileUploaded")
		s.index.EnqueueEnrich(ev.Ref, search.EnrichPriorityNormal, "event:FileUploaded", false, false)
		e.Ack()
	case events.UploadReady:
		s.index.EnqueueIndex(ev.FileRef, "event:UploadReady")
		s.index.EnqueueEnrich(ev.FileRef, search.EnrichPriorityNormal, "event:UploadReady", false, false)
		e.Ack()
	case events.SpaceRenamed:
		// Space rename: no single ref, handled by IndexSpace separately
		e.Ack()
	case events.LabelAdded:
		s.index.EnqueueIndex(ev.Ref, "event:LabelAdded")
		e.Ack()
	case events.LabelRemoved:
		s.index.EnqueueIndex(ev.Ref, "event:LabelRemoved")
		e.Ack()
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
