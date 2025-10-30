package identity

import (
	"context"

	cs3User "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cache", func() {
	var (
		ctx  context.Context
		idc  IdentityCache
		alan = cs3User.User{
			Id: &cs3User.UserId{
				OpaqueId: "alan",
				TenantId: "",
			},
			DisplayName: "Alan",
		}
	)
	BeforeEach(func() {
		idc = NewIdentityCache()
		ctx = context.Background()
	})

	Describe("GetUser", func() {
		It("should return not error", func() {
			// Persist the user to the cache for 1 hour
			idc.users.Set(alan.GetId().OpaqueId, &alan, 3600)

			ru, err := idc.GetUser(ctx, "", "alan")
			Expect(err).To(BeNil())
			Expect(ru).ToNot(BeNil())
			Expect(ru.GetId()).To(Equal(alan.GetId()))
			Expect(ru.GetDisplayName()).To(Equal(alan.GetDisplayName()))
		})

		It("should return an error, if the tenant id does not match", func() {
			alan.GetId().TenantId = "1234"
			// Persist the user to the cache for 1 hour
			idc.users.Set(alan.GetId().OpaqueId, &alan, 3600)
			_, err := idc.GetUser(ctx, "5678", "alan")
			Expect(err).ToNot(BeNil())
		})
	})
})
