package cache

import (
	"context"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	cs3User "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
)

// mockGatewaySelector is a mock implementation of pool.Selectable[gateway.GatewayAPIClient]
type mockGatewaySelector struct {
	client gateway.GatewayAPIClient
}

func (m *mockGatewaySelector) Next(opts ...pool.Option) (gateway.GatewayAPIClient, error) {
	return m.client, nil
}

var _ = Describe("Cache", func() {
	var (
		ctx            context.Context
		idc            IdentityCache
		mockGwSelector pool.Selectable[gateway.GatewayAPIClient]
	)

	BeforeEach(func() {
		// Create a mock gateway selector (client can be nil for cached tests)
		mockGwSelector = &mockGatewaySelector{
			client: nil,
		}

		idc = NewIdentityCache(
			IdentityCacheWithGatewaySelector(mockGwSelector),
		)
		ctx = context.Background()
	})

	Describe("GetUser", func() {
		It("should return no error", func() {
			alan := &cs3User.User{
				Id: &cs3User.UserId{
					OpaqueId: "alan",
					TenantId: "",
				},
				DisplayName: "Alan",
			}
			// Persist the user to the cache for 1 hour
			idc.users.Set(alan.GetId().GetTenantId()+"|"+alan.GetId().GetOpaqueId(), alan, 3600)

			// getting the cache item in cache.go line 103 does not work
			ru, err := idc.GetUser(ctx, "", "alan")
			Expect(err).To(BeNil())
			Expect(ru).ToNot(BeNil())
			Expect(ru.GetId()).To(Equal(alan.GetId().GetOpaqueId()))
			Expect(ru.GetDisplayName()).To(Equal(alan.GetDisplayName()))
		})

		It("should return an error, if the tenant id does not match", func() {
			alan := &cs3User.User{
				Id: &cs3User.UserId{
					OpaqueId: "alan",
					TenantId: "1234",
				},
				DisplayName: "Alan",
			}
			// Persist the user to the cache for 1 hour
			idc.users.Set(alan.GetId().GetTenantId()+"|"+alan.GetId().GetOpaqueId(), alan, 3600)
			_, err := idc.GetUser(ctx, "5678", "alan")
			Expect(err).ToNot(BeNil())
		})

		It("should not return an errorr, if the tenant id does match", func() {
			alan := &cs3User.User{
				Id: &cs3User.UserId{
					OpaqueId: "alan",
					TenantId: "1234",
				},
				DisplayName: "Alan",
			}
			// Persist the user to the cache for 1 hour
			cu := idc.users.Set(alan.GetId().GetTenantId()+"|"+alan.GetId().GetOpaqueId(), alan, 3600)
			// Test if element has been persisted in the cache
			Expect(cu.Value().GetId().GetOpaqueId()).To(Equal(alan.GetId().GetOpaqueId()))
			ru, err := idc.GetUser(ctx, "1234", "alan")
			Expect(err).To(BeNil())
			Expect(ru.GetDisplayName()).To(Equal(alan.GetDisplayName()))
		})
	})
})
