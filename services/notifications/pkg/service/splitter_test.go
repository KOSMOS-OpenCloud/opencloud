package service

import (
	"context"
	"strings"

	user "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/mock"
	"go-micro.dev/v4/client"

	"github.com/opencloud-eu/opencloud/pkg/log"
	settingsmsg "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/messages/settings/v0"
	settings "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/settings/v0"
	settingsmocks "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/settings/v0/mocks"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NotificationFilter", func() {
	var (
		testLogger = log.NewLogger()

		vs *settingsmocks.ValueService
		s  intervalSplitter
	)

	BeforeEach(func() {
		vs = &settingsmocks.ValueService{}
		s = intervalSplitter{
			log:         testLogger,
			valueClient: vs,
		}
	})

	It("handles connection errors", func() {
		vs.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(nil, errors.New("no connection to ValueService"))

		instant, daily, weekly := s.execute(context.TODO(), newUsers("foo"))
		Expect(instant).To(Equal(newUsers("foo")))
		Expect(daily).To(BeEmpty())
		Expect(weekly).To(BeEmpty())
	})

	It("handles no setting in ValueService response", func() {
		vs.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(&settings.GetValueResponse{}, nil)

		instant, daily, weekly := s.execute(context.TODO(), newUsers("foo"))
		Expect(instant).To(Equal(newUsers("foo")))
		Expect(daily).To(BeEmpty())
		Expect(weekly).To(BeEmpty())
	})

	It("handles nil response", func() {
		vs.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(nil, nil)

		instant, daily, weekly := s.execute(context.TODO(), newUsers("foo"))
		Expect(instant).To(Equal(newUsers("foo")))
		Expect(daily).To(BeEmpty())
		Expect(weekly).To(BeEmpty())
	})

	It("handles nil input user", func() {
		vs.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(nil, nil)

		instant, daily, weekly := s.execute(context.TODO(), nil)
		Expect(instant).To(BeEmpty())
		Expect(daily).To(BeEmpty())
		Expect(weekly).To(BeEmpty())
	})

	It("handles never interval", func() {
		vs.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(newGetValueResponseStringValue("never"), nil)

		instant, daily, weekly := s.execute(context.TODO(), newUsers("foo"))
		Expect(instant).To(BeEmpty())
		Expect(daily).To(BeEmpty())
		Expect(weekly).To(BeEmpty())
	})

	It("handles instant interval", func() {
		vs.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(newGetValueResponseStringValue("instant"), nil)

		instant, daily, weekly := s.execute(context.TODO(), newUsers("foo"))
		Expect(instant).To(Equal(newUsers("foo")))
		Expect(daily).To(BeEmpty())
		Expect(weekly).To(BeEmpty())
	})

	It("handles daily interval", func() {
		vs.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(newGetValueResponseStringValue("daily"), nil)

		instant, daily, weekly := s.execute(context.TODO(), newUsers("foo"))
		Expect(instant).To(BeEmpty())
		Expect(daily).To(Equal(newUsers("foo")))
		Expect(weekly).To(BeEmpty())
	})

	It("handles weekly interval", func() {
		vs.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(newGetValueResponseStringValue("weekly"), nil)

		instant, daily, weekly := s.execute(context.TODO(), newUsers("foo"))
		Expect(instant).To(BeEmpty())
		Expect(daily).To(BeEmpty())
		Expect(weekly).To(Equal(newUsers("foo")))
	})

	It("handles multiple users and intervals", func() {
		vs.On("GetValueByUniqueIdentifiers", mock.Anything, mock.Anything).Return(func(ctx context.Context, req *settings.GetValueByUniqueIdentifiersRequest, opts ...client.CallOption) *settings.GetValueResponse {
			if strings.Contains(req.AccountUuid, "never") {
				return newGetValueResponseStringValue("never")
			} else if strings.Contains(req.AccountUuid, "instant") {
				return newGetValueResponseStringValue("instant")
			} else if strings.Contains(req.AccountUuid, "daily") {
				return newGetValueResponseStringValue("daily")
			} else if strings.Contains(req.AccountUuid, "weekly") {
				return newGetValueResponseStringValue("weekly")
			}
			return nil
		}, nil)

		instant, daily, weekly := s.execute(context.TODO(), newUsers("never1", "instant1", "daily1", "weekly1", "never2", "instant2", "daily2", "weekly2"))
		Expect(instant).To(Equal(newUsers("instant1", "instant2")))
		Expect(daily).To(Equal(newUsers("daily1", "daily2")))
		Expect(weekly).To(Equal(newUsers("weekly1", "weekly2")))
	})
})

func newGetValueResponseStringValue(strVal string) *settings.GetValueResponse {
	return &settings.GetValueResponse{Value: &settingsmsg.ValueWithIdentifier{
		Value: &settingsmsg.Value{
			Value: &settingsmsg.Value_StringValue{
				StringValue: strVal,
			},
		},
	}}
}

func newUsers(ids ...string) []*user.User {
	var users []*user.User
	for _, s := range ids {
		users = append(users, &user.User{Id: &user.UserId{OpaqueId: s}})
	}
	return users
}
