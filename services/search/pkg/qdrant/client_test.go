package qdrant

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencloud-eu/opencloud/pkg/log"
)

type recordedRequest struct {
	method string
	path   string
	body   map[string]interface{}
}

// newRecordingServer answers every request with response and notes it.
func newRecordingServer(t *testing.T, response string) (*Client, *[]recordedRequest) {
	t.Helper()
	requests := &[]recordedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ := io.ReadAll(r.Body)
		body := map[string]interface{}{}
		_ = json.Unmarshal(rawBody, &body)
		*requests = append(*requests, recordedRequest{method: r.Method, path: r.URL.Path, body: body})
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return New(server.URL, "opencloud", log.NewLogger()), requests
}

func asJSON(t *testing.T, value interface{}) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestDelete(t *testing.T) {
	client, requests := newRecordingServer(t, `{"status":"ok"}`)
	if err := client.Delete([]string{"id-a", "id-b"}); err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 1 {
		t.Fatalf("requests: %+v", *requests)
	}
	request := (*requests)[0]
	if request.method != "POST" || request.path != "/collections/opencloud/points/delete" {
		t.Fatalf("request: %+v", request)
	}
	if got, want := asJSON(t, request.body), `{"filter":{"must":[{"has_id":["id-a","id-b"]}]}}`; got != want {
		t.Fatalf("body %s, want %s", got, want)
	}
}

func TestDeleteSplitsLargeLists(t *testing.T) {
	client, requests := newRecordingServer(t, `{"status":"ok"}`)
	ids := make([]string, pointsPerRequest+1)
	for position := range ids {
		ids[position] = "id"
	}
	if err := client.Delete(ids); err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 2 {
		t.Fatalf("got %d requests, want 2", len(*requests))
	}
}

func TestSetPayload(t *testing.T) {
	client, requests := newRecordingServer(t, `{"status":"ok"}`)
	err := client.SetPayload([]PayloadUpdate{
		{IDs: []string{"id-a"}, Payload: map[string]interface{}{"path": "./new/a.pdf", "name": "a.pdf"}},
		{IDs: []string{"id-b", "id-c"}, Payload: map[string]interface{}{"deleted": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 1 {
		t.Fatalf("requests: %+v", *requests)
	}
	request := (*requests)[0]
	if request.method != "POST" || request.path != "/collections/opencloud/points/batch" {
		t.Fatalf("request: %+v", request)
	}
	want := `{"operations":[` +
		`{"set_payload":{"filter":{"must":[{"has_id":["id-a"]}]},"payload":{"name":"a.pdf","path":"./new/a.pdf"}}},` +
		`{"set_payload":{"filter":{"must":[{"has_id":["id-b","id-c"]}]},"payload":{"deleted":true}}}]}`
	if got := asJSON(t, request.body); got != want {
		t.Fatalf("body %s, want %s", got, want)
	}
}

func TestRetrieve(t *testing.T) {
	client, requests := newRecordingServer(t, `{"result":[{"id":"id-a","payload":{"path":"./a.pdf"},"vector":[0.5,0.25]}]}`)
	point, found, err := client.Retrieve("id-a")
	if err != nil || !found {
		t.Fatalf("found %v, err %v", found, err)
	}
	if point.ID != "id-a" || len(point.Vector) != 2 || point.Vector[1] != 0.25 || point.Payload["path"] != "./a.pdf" {
		t.Fatalf("point: %+v", point)
	}
	request := (*requests)[0]
	if request.path != "/collections/opencloud/points" {
		t.Fatalf("request: %+v", request)
	}
	if got, want := asJSON(t, request.body), `{"ids":["id-a"],"with_payload":true,"with_vector":true}`; got != want {
		t.Fatalf("body %s, want %s", got, want)
	}
}

func TestRetrieveMissingPoint(t *testing.T) {
	client, _ := newRecordingServer(t, `{"result":[]}`)
	_, found, err := client.Retrieve("id-a")
	if err != nil || found {
		t.Fatalf("found %v, err %v", found, err)
	}
}

func TestSearchSkipsDeletedPoints(t *testing.T) {
	client, requests := newRecordingServer(t, `{"result":[]}`)
	if _, err := client.Search([]float64{0.5}, 20); err != nil {
		t.Fatal(err)
	}
	filter := asJSON(t, (*requests)[0].body["filter"])
	if want := `{"must_not":[{"key":"deleted","match":{"value":true}}]}`; filter != want {
		t.Fatalf("filter %s, want %s", filter, want)
	}
}

func TestErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"status":{"error":"boom"}}`, http.StatusBadRequest)
	}))
	defer server.Close()
	client := New(server.URL, "opencloud", log.NewLogger())
	if err := client.Delete([]string{"id-a"}); err == nil {
		t.Fatal("expected an error")
	}
}
