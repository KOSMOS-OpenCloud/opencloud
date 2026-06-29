package service

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	libregraph "github.com/opencloud-eu/libre-graph-api-go"
)

var _ = Describe("parseTimeRange", func() {
	It("parses 7d", func() {
		cutoff, err := parseTimeRange("7d")
		Expect(err).ToNot(HaveOccurred())
		Expect(time.Since(cutoff)).To(BeNumerically("~", 7*24*time.Hour, time.Minute))
	})

	It("parses 1m", func() {
		cutoff, err := parseTimeRange("1m")
		Expect(err).ToNot(HaveOccurred())
		expected := time.Now().AddDate(0, -1, 0)
		Expect(cutoff).To(BeTemporally("~", expected, time.Minute))
	})

	It("parses 3m", func() {
		cutoff, err := parseTimeRange("3m")
		Expect(err).ToNot(HaveOccurred())
		expected := time.Now().AddDate(0, -3, 0)
		Expect(cutoff).To(BeTemporally("~", expected, time.Minute))
	})

	It("parses 6m", func() {
		cutoff, err := parseTimeRange("6m")
		Expect(err).ToNot(HaveOccurred())
		expected := time.Now().AddDate(0, -6, 0)
		Expect(cutoff).To(BeTemporally("~", expected, time.Minute))
	})

	It("parses 1y", func() {
		cutoff, err := parseTimeRange("1y")
		Expect(err).ToNot(HaveOccurred())
		expected := time.Now().AddDate(-1, 0, 0)
		Expect(cutoff).To(BeTemporally("~", expected, time.Minute))
	})

	It("rejects unknown values", func() {
		_, err := parseTimeRange("2w")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported timerange"))
	})

	It("rejects empty values", func() {
		_, err := parseTimeRange("")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("getFilters", func() {
	var svc *ActivitylogService

	BeforeEach(func() {
		svc = &ActivitylogService{}
	})

	It("parses timerange filter", func() {
		f, err := svc.getFilters("itemid:storageid$spaceid!base AND timerange:7d")
		Expect(err).ToNot(HaveOccurred())
		Expect(f.groupBy).To(BeEmpty())

		// Activity from 3 days ago should pass
		recent := RawActivity{EventID: "e1", Depth: 0, Timestamp: time.Now().Add(-3 * 24 * time.Hour)}
		Expect(f.rawFilter(recent)).To(BeTrue())

		// Activity from 10 days ago should be filtered
		old := RawActivity{EventID: "e2", Depth: 0, Timestamp: time.Now().Add(-10 * 24 * time.Hour)}
		Expect(f.rawFilter(old)).To(BeFalse())
	})

	It("parses groupby:user", func() {
		f, err := svc.getFilters("itemid:storageid$spaceid!base AND groupby:user")
		Expect(err).ToNot(HaveOccurred())
		Expect(f.groupBy).To(Equal("user"))
	})

	It("parses groupby:container", func() {
		f, err := svc.getFilters("itemid:storageid$spaceid!base AND groupby:container")
		Expect(err).ToNot(HaveOccurred())
		Expect(f.groupBy).To(Equal("container"))
	})

	It("rejects invalid groupby", func() {
		_, err := svc.getFilters("itemid:storageid$spaceid!base AND groupby:invalid")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported groupby"))
	})

	It("combines timerange and groupby", func() {
		f, err := svc.getFilters("itemid:storageid$spaceid!base AND timerange:1m AND groupby:user AND sort:desc AND limit:50")
		Expect(err).ToNot(HaveOccurred())
		Expect(f.groupBy).To(Equal("user"))
		Expect(f.limit).To(Equal(50))
	})
})

var _ = Describe("groupActivities", func() {
	var svc *ActivitylogService

	BeforeEach(func() {
		svc = &ActivitylogService{}
	})

	makeActivity := func(id string, userID string, userName string, resID string, resName string, ts time.Time) libregraph.Activity {
		return libregraph.Activity{
			Id:    id,
			Times: libregraph.ActivityTimes{RecordedTime: ts},
			Template: libregraph.ActivityTemplate{
				Message: "test",
				Variables: map[string]any{
					"user":     map[string]any{"id": userID, "displayName": userName},
					"resource": map[string]any{"id": resID, "name": resName},
				},
			},
		}
	}

	It("groups by user", func() {
		now := time.Now()
		activities := []libregraph.Activity{
			makeActivity("e1", "alice", "Alice", "r1", "Doc.pdf", now),
			makeActivity("e2", "bob", "Bob", "r2", "Plan.xlsx", now),
			makeActivity("e3", "alice", "Alice", "r3", "Notes.md", now),
		}

		result := svc.groupActivities(activities, "user")
		Expect(result.GroupBy).To(Equal("user"))
		Expect(result.Groups).To(HaveLen(2))

		// Alice should have 2 activities
		Expect(result.Groups[0].Key).To(Equal("user:alice"))
		Expect(result.Groups[0].Label).To(Equal("Alice"))
		Expect(result.Groups[0].Count).To(Equal(2))
		Expect(result.Groups[0].Activities).To(HaveLen(2))

		// Bob should have 1 activity
		Expect(result.Groups[1].Key).To(Equal("user:bob"))
		Expect(result.Groups[1].Label).To(Equal("Bob"))
		Expect(result.Groups[1].Count).To(Equal(1))
	})

	It("groups by container", func() {
		now := time.Now()
		activities := []libregraph.Activity{
			makeActivity("e1", "alice", "Alice", "folder-a", "Finanzen", now),
			makeActivity("e2", "bob", "Bob", "folder-b", "Personal", now),
			makeActivity("e3", "alice", "Alice", "folder-a", "Finanzen", now),
		}

		result := svc.groupActivities(activities, "container")
		Expect(result.GroupBy).To(Equal("container"))
		Expect(result.Groups).To(HaveLen(2))

		Expect(result.Groups[0].Key).To(Equal("resource:folder-a"))
		Expect(result.Groups[0].Label).To(Equal("Finanzen"))
		Expect(result.Groups[0].Count).To(Equal(2))

		Expect(result.Groups[1].Key).To(Equal("resource:folder-b"))
		Expect(result.Groups[1].Label).To(Equal("Personal"))
		Expect(result.Groups[1].Count).To(Equal(1))
	})

	It("puts activities without grouping key into 'other'", func() {
		activities := []libregraph.Activity{
			{
				Id:    "e1",
				Times: libregraph.ActivityTimes{RecordedTime: time.Now()},
				Template: libregraph.ActivityTemplate{
					Message:   "test",
					Variables: map[string]any{},
				},
			},
		}

		result := svc.groupActivities(activities, "user")
		Expect(result.Groups).To(HaveLen(1))
		Expect(result.Groups[0].Key).To(Equal("other"))
	})

	It("preserves insertion order", func() {
		now := time.Now()
		activities := []libregraph.Activity{
			makeActivity("e1", "charlie", "Charlie", "r1", "X", now),
			makeActivity("e2", "alice", "Alice", "r2", "Y", now),
			makeActivity("e3", "bob", "Bob", "r3", "Z", now),
		}

		result := svc.groupActivities(activities, "user")
		Expect(result.Groups[0].Key).To(Equal("user:charlie"))
		Expect(result.Groups[1].Key).To(Equal("user:alice"))
		Expect(result.Groups[2].Key).To(Equal("user:bob"))
	})
})
