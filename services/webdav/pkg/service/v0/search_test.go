package svc

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSearch(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Search Suite")
}

var _ = Describe("SpacesSearchRegex", func() {
	DescribeTable("path matching",
		func(path, expectedSpace string, shouldMatch bool) {
			matches := spacesSearchRegex.FindStringSubmatch(path)
			if shouldMatch {
				Expect(matches).ToNot(BeNil(), "Expected path %q to match", path)
				Expect(matches[1]).To(Equal(expectedSpace), "Expected space to be %q", expectedSpace)
			} else {
				Expect(matches).To(BeNil(), "Expected path %q not to match", path)
			}
		},
		Entry("standard dav spaces path", "/dav/spaces/12345", "12345", true),
		Entry("remote.php dav spaces path", "/remote.php/dav/spaces/12345", "12345", true),
		Entry("standard dav spaces path with subpaths", "/dav/spaces/12345/some/folder", "12345", true),
		Entry("remote.php dav spaces path with subpaths", "/remote.php/dav/spaces/12345/some/folder", "12345", true),
		Entry("standard dav spaces path without space", "/dav/spaces/", "", false),
		Entry("remote.php dav spaces path without space", "/remote.php/dav/spaces/", "", false),
		Entry("prefix match only", "/dav/spaces", "", false),
		Entry("unrelated path", "/dav/files/123", "", false),
	)
})
