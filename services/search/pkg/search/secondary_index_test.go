package search

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/search/pkg/qdrant"
)

// recordingSecondaryIndex notes every call as "operation id,id".
type recordingSecondaryIndex struct {
	calls []string
	err   error
}

func resourceIDs(resources []*Resource) string {
	ids := make([]string, 0, len(resources))
	for _, resource := range resources {
		ids = append(ids, resource.ID)
	}
	return strings.Join(ids, ",")
}

func (r *recordingSecondaryIndex) Name() string { return "recording" }

func (r *recordingSecondaryIndex) SetDeleted(resources []*Resource, deleted bool) error {
	operation := "restore "
	if deleted {
		operation = "delete "
	}
	r.calls = append(r.calls, operation+resourceIDs(resources))
	return r.err
}

func (r *recordingSecondaryIndex) Purge(resources []*Resource) error {
	r.calls = append(r.calls, "purge "+resourceIDs(resources))
	return r.err
}

func (r *recordingSecondaryIndex) Move(resources []*Resource) error {
	moved := make([]string, 0, len(resources))
	for _, resource := range resources {
		moved = append(moved, resource.ID+"@"+resource.Path)
	}
	r.calls = append(r.calls, "move "+strings.Join(moved, ","))
	return r.err
}

func (r *recordingSecondaryIndex) Rekey(oldID string, target *Resource) error {
	r.calls = append(r.calls, "rekey "+oldID+" -> "+target.ID+"@"+target.Path+" name="+target.Name)
	return r.err
}

// resolvingEngine is a recordingEngine that also knows what is below a container.
type resolvingEngine struct {
	recordingEngine
	affected map[string][]*Resource
}

func (e *resolvingEngine) AffectedResources(id string) ([]*Resource, error) {
	resources, ok := e.affected[id]
	if !ok {
		return nil, errors.New("entity not found")
	}
	return resources, nil
}

func TestAffectedResourcesOfService(t *testing.T) {
	folderContent := []*Resource{{ID: "s$a!folder"}, {ID: "s$a!child", Deleted: true}}

	t.Run("no lookup without a secondary index", func(t *testing.T) {
		s := &Service{engine: &resolvingEngine{affected: map[string][]*Resource{"s$a!folder": folderContent}}, logger: log.NewLogger()}
		if resources := s.affectedResources(&Resource{ID: "s$a!folder"}); resources != nil {
			t.Fatalf("got %v", resources)
		}
	})

	t.Run("engine resolves the container", func(t *testing.T) {
		s := &Service{
			engine:           &resolvingEngine{affected: map[string][]*Resource{"s$a!folder": folderContent}},
			logger:           log.NewLogger(),
			secondaryIndexes: []SecondaryIndex{&recordingSecondaryIndex{}},
		}
		if got := resourceIDs(s.affectedResources(&Resource{ID: "s$a!folder"})); got != "s$a!folder,s$a!child" {
			t.Fatalf("got %s", got)
		}
		if got := resourceIDs(onlyDeleted(s.affectedResources(&Resource{ID: "s$a!folder"}))); got != "s$a!child" {
			t.Fatalf("got %s", got)
		}
	})

	t.Run("resource alone if the engine does not know it or cannot resolve", func(t *testing.T) {
		secondaryIndexes := []SecondaryIndex{&recordingSecondaryIndex{}}
		unknown := &Service{engine: &resolvingEngine{}, logger: log.NewLogger(), secondaryIndexes: secondaryIndexes}
		if got := resourceIDs(unknown.affectedResources(&Resource{ID: "s$a!gone"})); got != "s$a!gone" {
			t.Fatalf("got %s", got)
		}
		noResolver := &Service{engine: &recordingEngine{}, logger: log.NewLogger(), secondaryIndexes: secondaryIndexes}
		if got := resourceIDs(noResolver.affectedResources(&Resource{ID: "s$a!file"})); got != "s$a!file" {
			t.Fatalf("got %s", got)
		}
	})
}

func TestSecondaryIndexesFollowTheQueue(t *testing.T) {
	const oldID, newID = "storage$spaceA!nodeA", "storage$spaceB!nodeB"

	t.Run("a failing index does not stop the others", func(t *testing.T) {
		failing := &recordingSecondaryIndex{err: errors.New("down")}
		healthy := &recordingSecondaryIndex{}
		s := &Service{engine: &resolvingEngine{}, logger: log.NewLogger(), secondaryIndexes: []SecondaryIndex{failing, healthy}}
		s.secondarySetDeleted(newID, true)
		s.secondarySetDeleted(newID, false)
		s.secondaryPurge(newID, []*Resource{{ID: newID}})
		s.secondaryPurge(newID, nil)
		want := "delete " + newID + "|restore " + newID + "|purge " + newID
		if got := strings.Join(healthy.calls, "|"); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
		if len(failing.calls) != 3 {
			t.Fatalf("failing index calls: %v", failing.calls)
		}
	})

	t.Run("move inside a space passes on the new paths", func(t *testing.T) {
		secondaryIndex := &recordingSecondaryIndex{}
		engine := &resolvingEngine{
			recordingEngine: recordingEngine{knownIDs: map[string]bool{newID: true}},
			affected: map[string][]*Resource{newID: {
				{ID: newID, Path: "./renamed"},
				{ID: "storage$spaceB!child", Path: "./renamed/child.pdf"},
			}},
		}
		s := &Service{engine: engine, logger: log.NewLogger(), secondaryIndexes: []SecondaryIndex{secondaryIndex}}
		s.moveInIndex(queueRequest{op: "move", resourceID: newID, path: "./renamed"}, nil)
		want := "move " + newID + "@./renamed,storage$spaceB!child@./renamed/child.pdf"
		if got := strings.Join(secondaryIndex.calls, "|"); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("cross-space move rekeys before the target is indexed", func(t *testing.T) {
		secondaryIndex := &recordingSecondaryIndex{}
		engine := &resolvingEngine{recordingEngine: recordingEngine{knownIDs: map[string]bool{oldID: true, newID: true}}}
		s := &Service{engine: engine, logger: log.NewLogger(), secondaryIndexes: []SecondaryIndex{secondaryIndex}}
		s.moveInIndex(queueRequest{op: "move", resourceID: newID, oldResourceID: oldID, path: "/sub/a.pdf"}, nil)
		want := "rekey " + oldID + " -> " + newID + "@./sub/a.pdf name=a.pdf|move " + newID + "@./sub/a.pdf"
		if got := strings.Join(secondaryIndex.calls, "|"); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})
}

func TestVectorPointID(t *testing.T) {
	const nodeID = "0b9d4a3e-6a53-4c9b-9a0f-1f2e3d4c5b6a"
	if pointID, ok := vectorPointID("storage$space!" + nodeID); !ok || pointID != nodeID {
		t.Fatalf("got %q, %v", pointID, ok)
	}
	for _, resourceID := range []string{"", "storage$space!not-a-uuid", "nonsense"} {
		if pointID, ok := vectorPointID(resourceID); ok {
			t.Fatalf("%q must have no point, got %q", resourceID, pointID)
		}
	}
}

func TestVectorIndexRekey(t *testing.T) {
	const (
		oldNodeID = "0b9d4a3e-6a53-4c9b-9a0f-1f2e3d4c5b6a"
		newNodeID = "7c1e2f30-1111-4222-8333-444455556666"
		spaceB    = "9f8e7d6c-aaaa-4bbb-8ccc-ddddeeeeffff"
	)
	type recordedCall struct {
		path string
		body string
	}
	newServer := func(retrieveResponse string) (*vectorIndex, *[]recordedCall) {
		calls := &[]recordedCall{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawBody, _ := io.ReadAll(r.Body)
			*calls = append(*calls, recordedCall{path: r.Method + " " + strings.TrimPrefix(r.URL.Path, "/collections/opencloud/"), body: string(rawBody)})
			if strings.HasSuffix(r.URL.Path, "/points") && r.Method == "POST" {
				_, _ = w.Write([]byte(retrieveResponse))
				return
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}))
		t.Cleanup(server.Close)
		return newVectorIndex(qdrant.New(server.URL, "opencloud", log.NewLogger())), calls
	}
	target := &Resource{ID: "storage$" + spaceB + "!" + newNodeID, Path: "./sub/a.pdf"}
	target.Name = "a.pdf"

	t.Run("copies the point to the new id and deletes the old one", func(t *testing.T) {
		index, calls := newServer(`{"result":[{"id":"` + oldNodeID + `","vector":[0.5,0.25],"payload":{"summary":"kept","path":"./old.pdf","name":"old.pdf","resource_id":"storage$spaceA!` + oldNodeID + `","root_id":"storage$spaceA!spaceA"}}]}`)
		if err := index.Rekey("storage$spaceA!"+oldNodeID, target); err != nil {
			t.Fatal(err)
		}
		if len(*calls) != 3 {
			t.Fatalf("calls: %+v", *calls)
		}
		if (*calls)[0].path != "POST points" || (*calls)[1].path != "PUT points" || (*calls)[2].path != "POST points/delete" {
			t.Fatalf("calls: %+v", *calls)
		}
		var upsert struct {
			Points []qdrant.Point `json:"points"`
		}
		if err := json.Unmarshal([]byte((*calls)[1].body), &upsert); err != nil || len(upsert.Points) != 1 {
			t.Fatalf("upsert body %s: %v", (*calls)[1].body, err)
		}
		point := upsert.Points[0]
		if point.ID != newNodeID || len(point.Vector) != 2 {
			t.Fatalf("point: %+v", point)
		}
		wantPayload := map[string]string{
			"summary":     "kept",
			"path":        "./sub/a.pdf",
			"name":        "a.pdf",
			"resource_id": target.ID,
			"root_id":     "storage$" + spaceB + "!" + spaceB,
		}
		for key, want := range wantPayload {
			if point.Payload[key] != want {
				t.Errorf("payload %s = %v, want %s", key, point.Payload[key], want)
			}
		}
		if !strings.Contains((*calls)[2].body, oldNodeID) {
			t.Fatalf("delete body: %s", (*calls)[2].body)
		}
	})

	t.Run("no old point, nothing is written", func(t *testing.T) {
		index, calls := newServer(`{"result":[]}`)
		if err := index.Rekey("storage$spaceA!"+oldNodeID, target); err != nil {
			t.Fatal(err)
		}
		if len(*calls) != 1 {
			t.Fatalf("calls: %+v", *calls)
		}
	})
}
