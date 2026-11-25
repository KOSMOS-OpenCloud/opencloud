package search

import (
	"github.com/google/uuid"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"

	"github.com/opencloud-eu/opencloud/services/search/pkg/qdrant"
)

// vectorIndex keeps the Qdrant points in step with the engine. A point has the
// node ID of its resource as point ID and carries resource_id, root_id, path
// and name in its payload. Points are written by the enrich worker.
type vectorIndex struct {
	client *qdrant.Client
}

var _ SecondaryIndex = (*vectorIndex)(nil)

func newVectorIndex(client *qdrant.Client) *vectorIndex {
	return &vectorIndex{client: client}
}

func (v *vectorIndex) Name() string {
	return "qdrant"
}

// vectorPointID returns the point ID of a resource ID (storageID$spaceID!nodeID).
// Qdrant only takes UUIDs, anything else has no point.
func vectorPointID(resourceID string) (string, bool) {
	parsedID, err := storagespace.ParseID(resourceID)
	if err != nil {
		return "", false
	}
	if _, err := uuid.Parse(parsedID.GetOpaqueId()); err != nil {
		return "", false
	}
	return parsedID.GetOpaqueId(), true
}

func vectorPointIDs(resources []*Resource) []string {
	pointIDs := make([]string, 0, len(resources))
	for _, resource := range resources {
		if pointID, ok := vectorPointID(resource.ID); ok {
			pointIDs = append(pointIDs, pointID)
		}
	}
	return pointIDs
}

func (v *vectorIndex) SetDeleted(resources []*Resource, deleted bool) error {
	pointIDs := vectorPointIDs(resources)
	if len(pointIDs) == 0 {
		return nil
	}
	return v.client.SetPayload([]qdrant.PayloadUpdate{{
		IDs:     pointIDs,
		Payload: map[string]interface{}{qdrant.DeletedPayloadKey: deleted},
	}})
}

func (v *vectorIndex) Purge(resources []*Resource) error {
	pointIDs := vectorPointIDs(resources)
	if len(pointIDs) == 0 {
		return nil
	}
	return v.client.Delete(pointIDs)
}

func (v *vectorIndex) Move(resources []*Resource) error {
	updates := make([]qdrant.PayloadUpdate, 0, len(resources))
	for _, resource := range resources {
		pointID, ok := vectorPointID(resource.ID)
		if !ok || resource.Path == "" {
			continue
		}
		updates = append(updates, qdrant.PayloadUpdate{
			IDs:     []string{pointID},
			Payload: map[string]interface{}{"path": resource.Path, "name": resource.Name},
		})
	}
	if len(updates) == 0 {
		return nil
	}
	return v.client.SetPayload(updates)
}

func (v *vectorIndex) Rekey(oldID string, target *Resource) error {
	oldPointID, ok := vectorPointID(oldID)
	if !ok {
		return nil
	}
	targetPointID, ok := vectorPointID(target.ID)
	if !ok || targetPointID == oldPointID {
		return nil
	}
	point, found, err := v.client.Retrieve(oldPointID)
	if err != nil || !found {
		return err
	}

	targetID, err := storagespace.ParseID(target.ID)
	if err != nil {
		return err
	}
	targetID.OpaqueId = targetID.GetSpaceId()

	point.ID = targetPointID
	if point.Payload == nil {
		point.Payload = map[string]interface{}{}
	}
	point.Payload["resource_id"] = target.ID
	point.Payload["root_id"] = storagespace.FormatResourceID(&targetID)
	point.Payload["path"] = target.Path
	point.Payload["name"] = target.Name
	if err := v.client.Upsert([]qdrant.Point{point}); err != nil {
		return err
	}
	return v.client.Delete([]string{oldPointID})
}
