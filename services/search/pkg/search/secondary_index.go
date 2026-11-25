package search

// SecondaryIndex is an index that is kept next to the Engine, for example the
// vector store. The Engine stays the main index: it is written first and knows
// which documents an operation touches (a container and everything below it).
// The index queue passes that list on, so a secondary index does not have to
// resolve containers itself.
//
// Documents enter a secondary index on their own path (the vector store is
// filled by the enrich worker). This interface covers what happens to them
// afterwards. Resources a secondary index does not hold are skipped without
// an error.
type SecondaryIndex interface {
	// Name identifies the index in log messages.
	Name() string
	// SetDeleted marks resources that moved to the trash bin (deleted true)
	// or came back from it (deleted false). Marked resources are no hits.
	SetDeleted(resources []*Resource, deleted bool) error
	// Purge removes resources for good.
	Purge(resources []*Resource) error
	// Move updates path and name of resources that kept their ID.
	Move(resources []*Resource) error
	// Rekey moves the entry of oldID to the ID of target. A cross-space move
	// gives a resource a new ID, target carries the new ID, path and name.
	Rekey(oldID string, target *Resource) error
}

// affectedResolver is implemented by engines that can list the documents an
// operation on a resource touches: the resource and, for a container, all
// documents below it.
type affectedResolver interface {
	AffectedResources(id string) ([]*Resource, error)
}

// affectedResources lists the documents an operation on resource touches. It
// has to run while the documents are still in the engine, so before a purge.
// Without a secondary index nothing is looked up. If the engine cannot resolve
// the resource, only the resource itself is returned.
func (s *Service) affectedResources(resource *Resource) []*Resource {
	if len(s.secondaryIndexes) == 0 {
		return nil
	}
	resolver, ok := s.engine.(affectedResolver)
	if !ok {
		return []*Resource{resource}
	}
	resources, err := resolver.AffectedResources(resource.ID)
	if err != nil {
		s.logger.Debug().Err(err).Str("id", resource.ID).Msg("secondary-index: resource not in the engine, passing on the resource alone")
		return []*Resource{resource}
	}
	return resources
}

// onlyDeleted keeps the resources that are marked as deleted.
func onlyDeleted(resources []*Resource) []*Resource {
	deletedResources := make([]*Resource, 0, len(resources))
	for _, resource := range resources {
		if resource.Deleted {
			deletedResources = append(deletedResources, resource)
		}
	}
	return deletedResources
}

// forSecondaryIndexes runs apply on every secondary index. A failing index is
// logged and does not stop the others or the index queue.
func (s *Service) forSecondaryIndexes(operation, id string, apply func(index SecondaryIndex) error) {
	for _, index := range s.secondaryIndexes {
		if err := apply(index); err != nil {
			s.logger.Error().Err(err).Str("index", index.Name()).Str("id", id).Msg("secondary-index: " + operation + " failed")
		}
	}
}

func (s *Service) secondarySetDeleted(id string, deleted bool) {
	resources := s.affectedResources(&Resource{ID: id})
	operation := "restore"
	if deleted {
		operation = "delete"
	}
	s.forSecondaryIndexes(operation, id, func(index SecondaryIndex) error {
		return index.SetDeleted(resources, deleted)
	})
}

func (s *Service) secondaryPurge(id string, resources []*Resource) {
	if len(resources) == 0 {
		return
	}
	s.forSecondaryIndexes("purge", id, func(index SecondaryIndex) error {
		return index.Purge(resources)
	})
}

func (s *Service) secondaryMove(id string, resources []*Resource) {
	s.forSecondaryIndexes("move", id, func(index SecondaryIndex) error {
		return index.Move(resources)
	})
}

func (s *Service) secondaryRekey(oldID string, target *Resource) {
	s.forSecondaryIndexes("rekey", oldID, func(index SecondaryIndex) error {
		return index.Rekey(oldID, target)
	})
}
