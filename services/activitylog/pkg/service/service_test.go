package service

import (
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/pkg/store"
)

var _ = Describe("ActivitylogService", func() {
	var alog *ActivitylogService
	var getResource func(ref *provider.Reference) (*provider.ResourceInfo, error)

	BeforeEach(func() {
		alog = &ActivitylogService{
			store: store.Create(),
		}
	})

	Describe("AddActivity", func() {
		type testCase struct {
			Name       string
			Tree       map[string]*provider.ResourceInfo
			Activities map[string]string
			Expected   map[string][]RawActivity
		}

		testCases := []testCase{
			{
				Name: "simple",
				Tree: map[string]*provider.ResourceInfo{
					"base":    resourceInfo("base", "parent"),
					"parent":  resourceInfo("parent", "spaceid"),
					"spaceid": resourceInfo("spaceid", "spaceid"),
				},
				Activities: map[string]string{
					"activity": "base",
				},
				Expected: map[string][]RawActivity{
					"base":    activitites("activity", 0),
					"parent":  activitites("activity", 1),
					"spaceid": activitites("activity", 2),
				},
			},
			{
				Name: "two activities on same resource",
				Tree: map[string]*provider.ResourceInfo{
					"base":    resourceInfo("base", "parent"),
					"parent":  resourceInfo("parent", "spaceid"),
					"spaceid": resourceInfo("spaceid", "spaceid"),
				},
				Activities: map[string]string{
					"activity1": "base",
					"activity2": "base",
				},
				Expected: map[string][]RawActivity{
					"base":    activitites("activity1", 0, "activity2", 0),
					"parent":  activitites("activity1", 1, "activity2", 1),
					"spaceid": activitites("activity1", 2, "activity2", 2),
				},
			},
			// Add other test cases here...
		}

		for _, tc := range testCases {
			tc := tc // capture range variable
			Context(tc.Name, func() {
				BeforeEach(func() {
					getResource = func(ref *provider.Reference) (*provider.ResourceInfo, error) {
						return tc.Tree[ref.GetResourceId().GetOpaqueId()], nil
					}

					for k, v := range tc.Activities {
						err := alog.addActivity(reference(v), k, time.Time{}, getResource)
						Expect(err).NotTo(HaveOccurred())
					}
				})

				It("should match the expected activities", func() {
					for id, acts := range tc.Expected {
						activities, err := alog.Activities(resourceID(id))
						Expect(err).NotTo(HaveOccurred(), tc.Name+":"+id)
						Expect(activities).To(ConsistOf(acts), tc.Name+":"+id)
					}
				})
			})
		}
	})
})

func activitites(acts ...interface{}) []RawActivity {
	var activities []RawActivity
	act := RawActivity{}
	for _, a := range acts {
		switch v := a.(type) {
		case string:
			act.EventID = v
		case int:
			act.Depth = v
			activities = append(activities, act)
		}
	}
	return activities
}

func resourceID(id string) *provider.ResourceId {
	return &provider.ResourceId{
		StorageId: "storageid",
		OpaqueId:  id,
		SpaceId:   "spaceid",
	}
}

func reference(id string) *provider.Reference {
	return &provider.Reference{ResourceId: resourceID(id)}
}

func resourceInfo(id, parentID string) *provider.ResourceInfo {
	return &provider.ResourceInfo{
		Id:       resourceID(id),
		ParentId: resourceID(parentID),
		Space: &provider.StorageSpace{
			Root: resourceID("spaceid"),
		},
	}
}
