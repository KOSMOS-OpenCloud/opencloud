package service

import (
	"context"
	"testing"

	user "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	"github.com/opencloud-eu/opencloud/pkg/log"
	settingsmsg "github.com/opencloud-eu/opencloud/protogen/gen/ocis/messages/settings/v0"
	settings "github.com/opencloud-eu/opencloud/protogen/gen/ocis/services/settings/v0"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"go-micro.dev/v4/client"
)

var testLogger = log.NewLogger()

func TestNotificationFilter_execute(t *testing.T) {
	type args struct {
		ctx       context.Context
		users     []*user.User
		settingId string
	}
	tests := []struct {
		name string
		vc   settings.ValueService
		args args
		want []*user.User
	}{
		{"no connection to ValueService", settings.MockValueService{
			GetValueByUniqueIdentifiersFunc: func(ctx context.Context, req *settings.GetValueByUniqueIdentifiersRequest, opts ...client.CallOption) (*settings.GetValueResponse, error) {
				return nil, errors.New("no connection to ValueService")
			},
		}, args{users: []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}, settingId: "bar", ctx: context.TODO()}, []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}},
		{"no setting in ValueService response", settings.MockValueService{
			GetValueByUniqueIdentifiersFunc: func(ctx context.Context, req *settings.GetValueByUniqueIdentifiersRequest, opts ...client.CallOption) (*settings.GetValueResponse, error) {
				return &settings.GetValueResponse{}, nil
			},
		}, args{users: []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}, settingId: "bar", ctx: context.TODO()}, []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}},
		{"ValueService nil response", settings.MockValueService{
			GetValueByUniqueIdentifiersFunc: func(ctx context.Context, req *settings.GetValueByUniqueIdentifiersRequest, opts ...client.CallOption) (*settings.GetValueResponse, error) {
				return nil, nil
			},
		}, args{users: []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}, settingId: "bar", ctx: context.TODO()}, []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}},
		{"Event enabled", setupMockValueService(true), args{users: []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}, settingId: "bar", ctx: context.TODO()}, []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}},
		{"Event disabled", setupMockValueService(false), args{users: []*user.User{{Id: &user.UserId{OpaqueId: "foo"}}}, settingId: "bar", ctx: context.TODO()}, []*user.User(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ulf := notificationFilter{
				log:         testLogger,
				valueClient: tt.vc,
			}
			assert.Equal(t, tt.want, ulf.execute(tt.args.ctx, tt.args.users, tt.args.settingId))
		})
	}
}

func setupMockValueService(mail bool) settings.ValueService {
	return settings.MockValueService{
		GetValueByUniqueIdentifiersFunc: func(ctx context.Context, req *settings.GetValueByUniqueIdentifiersRequest, opts ...client.CallOption) (*settings.GetValueResponse, error) {
			return &settings.GetValueResponse{
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
			}, nil
		},
	}
}
