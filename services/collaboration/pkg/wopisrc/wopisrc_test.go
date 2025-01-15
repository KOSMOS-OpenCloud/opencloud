package wopisrc_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/opencloud/services/collaboration/pkg/config"
	"github.com/opencloud-eu/opencloud/services/collaboration/pkg/wopisrc"
)

var _ = Describe("Wopisrc Test", func() {
	var (
		c *config.Config
	)

	Context("GenerateWopiSrc", func() {
		BeforeEach(func() {
			c = &config.Config{
				Wopi: config.Wopi{
					WopiSrc:     "https://cloud.example.test/wopi/files",
					ProxyURL:    "https://cloud.proxy.com",
					ProxySecret: "secret",
				},
			}
		})
		When("WopiSrc URL is incorrect", func() {
			c = &config.Config{
				Wopi: config.Wopi{
					WopiSrc: "https:&//cloud.example.test/wopi/files",
				},
			}
			url, err := wopisrc.GenerateWopiSrc("123456", c)
			Expect(err).To(HaveOccurred())
			Expect(url).To(BeNil())
		})
		When("proxy URL is incorrect", func() {
			c = &config.Config{
				Wopi: config.Wopi{
					WopiSrc:     "https://cloud.example.test/wopi/files",
					ProxyURL:    "cloud",
					ProxySecret: "secret",
				},
			}
			url, err := wopisrc.GenerateWopiSrc("123456", c)
			Expect(err).To(HaveOccurred())
			Expect(url).To(BeNil())
		})
		When("proxy URL and proxy secret are configured", func() {
			It("should generate a WOPI src URL as a jwt token", func() {
				url, err := wopisrc.GenerateWopiSrc("123456", c)
				Expect(err).ToNot(HaveOccurred())
				Expect(url.String()).To(Equal("https://cloud.proxy.com/wopi/files/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1IjoiaHR0cHM6Ly9jbG91ZC5leGFtcGxlLnRlc3Qvd29waS9maWxlcy8iLCJmIjoiMTIzNDU2In0.LzyGPanHKxjLlIPoyfGU4cAUxzy3FAmBqMIqLCSHclg"))
			})
		})
		When("proxy URL and proxy secret are not configured", func() {
			It("should generate a WOPI src URL as a direct URL", func() {
				c.Wopi.ProxyURL = ""
				c.Wopi.ProxySecret = ""
				url, err := wopisrc.GenerateWopiSrc("123456", c)
				Expect(err).ToNot(HaveOccurred())
				Expect(url.String()).To(Equal("https://cloud.example.test/wopi/files/123456"))
			})
		})
	})
})
