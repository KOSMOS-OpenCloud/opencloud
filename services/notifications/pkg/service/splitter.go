package service

import (
	"context"

	user "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/pkg/middleware"
	settingssvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/settings/v0"
	"github.com/opencloud-eu/opencloud/services/settings/pkg/store/defaults"
	"github.com/pkg/errors"
	micrometadata "go-micro.dev/v4/metadata"
)

type intervalSplitter struct {
	log         log.Logger
	valueClient settingssvc.ValueService
}

func newIntervalSplitter(l log.Logger, vc settingssvc.ValueService) *intervalSplitter {
	return &intervalSplitter{log: l, valueClient: vc}
}

// execute splits users into 3 lists depending on their email sending interval settings
func (s intervalSplitter) execute(ctx context.Context, users []*user.User) (instant, daily, weekly []*user.User) {
	for _, u := range users {
		userId := u.GetId().GetOpaqueId()
		interval, err := getEmailSendingInterval(ctx, s.valueClient, userId)
		if err != nil {
			s.log.Error().Err(err).Str("userId", userId).Msg("cannot get user email sending interval")
			instant = append(instant, u)
		} else if interval == "instant" {
			instant = append(instant, u)
		} else if interval == _intervalDaily {
			daily = append(daily, u)
		} else if interval == _intervalWeekly {
			weekly = append(weekly, u)
		}
	}
	return
}

func getEmailSendingInterval(ctx context.Context, vc settingssvc.ValueService, userId string) (string, error) {
	resp, err := vc.GetValueByUniqueIdentifiers(
		micrometadata.Set(ctx, middleware.AccountID, userId),
		&settingssvc.GetValueByUniqueIdentifiersRequest{
			AccountUuid: userId,
			SettingId:   defaults.SettingUUIDProfileEmailSendingInterval,
		},
	)

	if err != nil {
		return "", err
	}

	val := resp.GetValue().GetValue().GetStringValue()
	if val == "" {
		return "", errors.New("email sending interval is empty")
	}
	return val, nil
}
