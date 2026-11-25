package search

import (
	"context"
	"errors"
	"testing"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	typesv1beta1 "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/status"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"google.golang.org/grpc"

	"github.com/opencloud-eu/opencloud/pkg/log"
)

// recordingEngine notes the calls of moveInIndex. Move fails for IDs that are
// not in knownIDs, like the bleve backend does for a missing document.
type recordingEngine struct {
	Engine
	knownIDs  map[string]bool
	purgedIDs []string
	movedIDs  []string
}

// stubGateway answers the three gateway calls indexing a resource needs.
type stubGateway struct {
	gateway.GatewayAPIClient
	targetInfo *provider.ResourceInfo
}

func (g *stubGateway) Authenticate(ctx context.Context, _ *gateway.AuthenticateRequest, _ ...grpc.CallOption) (*gateway.AuthenticateResponse, error) {
	return &gateway.AuthenticateResponse{Status: status.NewOK(ctx), Token: "authtoken"}, nil
}

func (g *stubGateway) Stat(ctx context.Context, _ *provider.StatRequest, _ ...grpc.CallOption) (*provider.StatResponse, error) {
	return &provider.StatResponse{Status: status.NewOK(ctx), Info: g.targetInfo}, nil
}

func (g *stubGateway) GetPath(ctx context.Context, _ *provider.GetPathRequest, _ ...grpc.CallOption) (*provider.GetPathResponse, error) {
	return &provider.GetPathResponse{Status: status.NewOK(ctx), Path: "/" + g.targetInfo.GetPath()}, nil
}

// recordingBatch notes the documents written to the batch.
type recordingBatch struct {
	BatchOperator
	upsertedIDs []string
}

func (b *recordingBatch) Upsert(id string, _ Resource) error {
	b.upsertedIDs = append(b.upsertedIDs, id)
	return nil
}

func (e *recordingEngine) Purge(id string, _ bool) error {
	e.purgedIDs = append(e.purgedIDs, id)
	if !e.knownIDs[id] {
		return errors.New("entity not found")
	}
	delete(e.knownIDs, id)
	return nil
}

func (e *recordingEngine) Move(id string, _ string, _ string) error {
	if !e.knownIDs[id] {
		return errors.New("entity not found")
	}
	e.movedIDs = append(e.movedIDs, id)
	return nil
}

func TestPreviousResourceID(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]string
		want     string
	}{
		{"never moved across spaces", map[string]string{"doc.type": "rechnung"}, ""},
		{"one cross-space move", map[string]string{"user.oc.oldids.0": "spaceA!nodeA"}, "storage$spaceA!nodeA"},
		{"newest entry wins", map[string]string{"user.oc.oldids.0": "spaceA!nodeA", "user.oc.oldids.1": "spaceB!nodeB"}, "storage$spaceB!nodeB"},
		{"already has a storage id", map[string]string{"user.oc.oldids.0": "other$spaceA!nodeA"}, "other$spaceA!nodeA"},
	}
	for _, tc := range cases {
		info := &provider.ResourceInfo{
			Id:                &provider.ResourceId{StorageId: "storage", SpaceId: "spaceC", OpaqueId: "nodeC"},
			ArbitraryMetadata: &provider.ArbitraryMetadata{Metadata: tc.metadata},
		}
		if got := previousResourceID(info); got != tc.want {
			t.Errorf("%s: previousResourceID = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestMoveInIndex(t *testing.T) {
	const oldID, newID = "storage$spaceA!nodeA", "storage$spaceB!nodeB"

	t.Run("move inside a space updates the document in place", func(t *testing.T) {
		engine := &recordingEngine{knownIDs: map[string]bool{newID: true}}
		s := &Service{engine: engine, logger: log.NewLogger()}
		if s.moveInIndex(queueRequest{op: "move", resourceID: newID, path: "./a.pdf"}, nil) {
			t.Fatal("nothing must be added to the batch")
		}
		if len(engine.purgedIDs) != 0 || len(engine.movedIDs) != 1 {
			t.Fatalf("purged %v, moved %v", engine.purgedIDs, engine.movedIDs)
		}
	})

	t.Run("later move of a resource that once changed spaces", func(t *testing.T) {
		engine := &recordingEngine{knownIDs: map[string]bool{newID: true}}
		s := &Service{engine: engine, logger: log.NewLogger()}
		if s.moveInIndex(queueRequest{op: "move", resourceID: newID, oldResourceID: oldID, path: "./a.pdf"}, nil) {
			t.Fatal("nothing must be added to the batch")
		}
		if len(engine.movedIDs) != 1 || !engine.knownIDs[newID] {
			t.Fatalf("moved %v, known %v", engine.movedIDs, engine.knownIDs)
		}
	})

	t.Run("move without old id and without document only fails", func(t *testing.T) {
		engine := &recordingEngine{knownIDs: map[string]bool{}}
		s := &Service{engine: engine, logger: log.NewLogger()}
		if s.moveInIndex(queueRequest{op: "move", resourceID: newID, path: "./a.pdf"}, nil) {
			t.Fatal("nothing must be added to the batch")
		}
		if len(engine.purgedIDs) != 0 {
			t.Fatalf("purged %v", engine.purgedIDs)
		}
	})

	t.Run("cross-space move purges the source document and indexes the target", func(t *testing.T) {
		targetID := &provider.ResourceId{StorageId: "storage", SpaceId: "spaceB", OpaqueId: "nodeB"}
		gatewayClient := &stubGateway{targetInfo: &provider.ResourceInfo{
			Id:       targetID,
			ParentId: &provider.ResourceId{StorageId: "storage", SpaceId: "spaceB", OpaqueId: "spaceB"},
			Type:     provider.ResourceType_RESOURCE_TYPE_FILE,
			Name:     "a.pdf",
			Path:     "a.pdf",
			Mtime:    &typesv1beta1.Timestamp{Seconds: 4000},
			ArbitraryMetadata: &provider.ArbitraryMetadata{
				Metadata: map[string]string{"user.oc.oldids.0": "spaceA!nodeA"},
			},
		}}
		pool.RemoveSelector("GatewaySelector" + "eu.opencloud.api.gateway")
		gatewaySelector := pool.GetSelector[gateway.GatewayAPIClient](
			"GatewaySelector",
			"eu.opencloud.api.gateway",
			func(cc grpc.ClientConnInterface) gateway.GatewayAPIClient {
				return gatewayClient
			},
		)

		engine := &recordingEngine{knownIDs: map[string]bool{oldID: true}}
		batch := &recordingBatch{}
		s := &Service{engine: engine, logger: log.NewLogger(), gatewaySelector: gatewaySelector}
		addedToBatch := s.moveInIndex(queueRequest{
			op:            "move",
			ref:           &provider.Reference{ResourceId: targetID},
			resourceID:    newID,
			oldResourceID: oldID,
			path:          "./a.pdf",
		}, batch)

		if !addedToBatch {
			t.Fatal("the target must be added to the batch")
		}
		if len(engine.purgedIDs) != 1 || engine.purgedIDs[0] != oldID || engine.knownIDs[oldID] {
			t.Fatalf("purged %v, known %v", engine.purgedIDs, engine.knownIDs)
		}
		if len(engine.movedIDs) != 0 {
			t.Fatalf("moved %v", engine.movedIDs)
		}
		if len(batch.upsertedIDs) != 1 || batch.upsertedIDs[0] != newID {
			t.Fatalf("upserted %v, want [%s]", batch.upsertedIDs, newID)
		}
	})
}
