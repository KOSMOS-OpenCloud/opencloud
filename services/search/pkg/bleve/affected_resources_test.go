package bleve_test

import (
	"sort"
	"testing"

	bleveSearch "github.com/blevesearch/bleve/v2"
	sprovider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"

	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/search/pkg/bleve"
	"github.com/opencloud-eu/opencloud/services/search/pkg/content"
	bleveQuery "github.com/opencloud-eu/opencloud/services/search/pkg/query/bleve"
	"github.com/opencloud-eu/opencloud/services/search/pkg/search"
)

func TestAffectedResources(t *testing.T) {
	mapping, err := bleve.NewMapping()
	if err != nil {
		t.Fatal(err)
	}
	index, err := bleveSearch.NewMemOnly(mapping)
	if err != nil {
		t.Fatal(err)
	}
	backend := bleve.NewBackend(index, bleveQuery.DefaultCreator, log.Logger{})

	container := uint64(sprovider.ResourceType_RESOURCE_TYPE_CONTAINER)
	file := uint64(sprovider.ResourceType_RESOURCE_TYPE_FILE)
	documents := []search.Resource{
		{ID: "1$2!folder", RootID: "1$2!2", Path: "./folder", Type: container, Document: content.Document{Name: "folder"}},
		{ID: "1$2!child", RootID: "1$2!2", Path: "./folder/child.pdf", Type: file, Document: content.Document{Name: "child.pdf"}},
		{ID: "1$2!trashed", RootID: "1$2!2", Path: "./folder/sub/trashed.pdf", Type: file, Deleted: true, Document: content.Document{Name: "trashed.pdf"}},
		{ID: "1$2!sibling", RootID: "1$2!2", Path: "./folder2/sibling.pdf", Type: file, Document: content.Document{Name: "sibling.pdf"}},
		{ID: "1$3!other", RootID: "1$3!3", Path: "./folder/other.pdf", Type: file, Document: content.Document{Name: "other.pdf"}},
	}
	for _, document := range documents {
		if err := backend.Upsert(document.ID, document); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("file returns itself", func(t *testing.T) {
		resources, err := backend.AffectedResources("1$2!child")
		if err != nil {
			t.Fatal(err)
		}
		if len(resources) != 1 || resources[0].ID != "1$2!child" || resources[0].Path != "./folder/child.pdf" || resources[0].Name != "child.pdf" {
			t.Fatalf("got %+v", resources)
		}
	})

	t.Run("container returns itself and everything below it in its space", func(t *testing.T) {
		resources, err := backend.AffectedResources("1$2!folder")
		if err != nil {
			t.Fatal(err)
		}
		deletedByID := map[string]bool{}
		resourceIDs := []string{}
		for _, resource := range resources {
			resourceIDs = append(resourceIDs, resource.ID)
			deletedByID[resource.ID] = resource.Deleted
		}
		sort.Strings(resourceIDs)
		want := []string{"1$2!child", "1$2!folder", "1$2!trashed"}
		if len(resourceIDs) != len(want) {
			t.Fatalf("got %v, want %v", resourceIDs, want)
		}
		for position := range want {
			if resourceIDs[position] != want[position] {
				t.Fatalf("got %v, want %v", resourceIDs, want)
			}
		}
		if !deletedByID["1$2!trashed"] || deletedByID["1$2!child"] {
			t.Fatalf("deleted flags wrong: %v", deletedByID)
		}
	})

	t.Run("unknown id is an error", func(t *testing.T) {
		if _, err := backend.AffectedResources("1$2!missing"); err == nil {
			t.Fatal("expected an error")
		}
	})
}
