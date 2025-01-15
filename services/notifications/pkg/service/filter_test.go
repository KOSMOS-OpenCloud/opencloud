package service

import (
	"context"

	user "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	"github.com/opencloud-eu/opencloud/pkg/log"
	settingsmsg "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/messages/settings/v0"
	settings "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/settings/v0"
	settingsmocks "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/settings/v0/mocks"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NotificationFilter", func() {
	var (
		testLogger = log.NewLogger()
		vs         = &settingsmocks.ValueService{}
	)

	setupMockValueService := func(mail bool) *settingsmocks.ValueService {
		m := &settingsmocks.ValueService{}
		m.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(&settings.GetValueResponse{
			Value: &settingsmsg.ValueWithIdentifier{
				Value: &settingsmsg.Value{
					Value: &settingsmsg.Value_CollectionValue{
						CollectionValue: &settingsmsg.CollectionValue{
							Values: []*settingsmsg.CollectionOption{
								{
									Key:    "mail",
									Option: &settingsmsg.CollectionOption_BoolValue{BoolValue: mail},
								},
							},
						},
					},
				},
			},
		}, nil)
		return m
	}

	Describe("execute", func() {
		It("handles connection errors", func() {
			vs.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(nil, errors.New("no connection to ValueService"))
			ulf := notificationFilter{
				log:         testLogger,
				valueClient: vs,
			}
			Expect(ulf.execute(context.TODO(), []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}, "bar")).To(ConsistOf(&user.User{Id: &user.UserId{OpaqueId: "foo"}}))
		})

		It("handles no setting in ValueService response", func() {
			vs.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(&settings.GetValueResponse{}, nil)
			ulf := notificationFilter{
				log:         testLogger,
				valueClient: vs,
			}
			Expect(ulf.execute(context.TODO(), []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}, "bar")).To(ConsistOf(&user.User{Id: &user.UserId{OpaqueId: "foo"}}))
		})

		It("handles nil responses", func() {
			vs.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(nil, nil)
			ulf := notificationFilter{
				log:         testLogger,
				valueClient: vs,
			}
			Expect(ulf.execute(context.TODO(), []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}, "bar")).To(ConsistOf(&user.User{Id: &user.UserId{OpaqueId: "foo"}}))
		})

		It("return users when events are enabled", func() {
			vs = setupMockValueService(true)
			ulf := notificationFilter{
				log:         testLogger,
				valueClient: vs,
			}
			Expect(ulf.execute(context.TODO(), []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}, "bar")).To(ConsistOf(&user.User{Id: &user.UserId{OpaqueId: "foo"}}))
		})

		It("return no users when events are disabled", func() {
			vs = setupMockValueService(false)
			ulf := notificationFilter{
				log:         testLogger,
				valueClient: vs,
			}
			Expect(ulf.execute(context.TODO(), []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}, "bar")).To(BeEmpty())
		})
	})
})
