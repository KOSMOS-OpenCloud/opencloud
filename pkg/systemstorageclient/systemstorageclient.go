package systemstorageclient

import (
	"context"
	"github.com/opencloud-eu/opencloud/pkg/log"

	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata"
)

type SystemDataStorageClient struct {
	mds *metadata.Storage
}

func (s SystemDataStorageClient) SimpleDownload(ctx context.Context, userID, identifier string) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (s SystemDataStorageClient) SimpleUpload(ctx context.Context, userID, identifier string, content []byte) error {
	//TODO implement me
	panic("implement me")
}

func (s SystemDataStorageClient) Delete(ctx context.Context, userID, identifier string) error {
	//TODO implement me
	panic("implement me")
}

func (s SystemDataStorageClient) ReadDir(ctx context.Context, userID, identifier string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (s SystemDataStorageClient) MakeDirIfNotExist(ctx context.Context, userID, identifier string) error {
	//TODO implement me
	panic("implement me")
}

func (s SystemDataStorageClient) Init(ctx context.Context, userID, identifier string) error {
	//TODO implement me
	panic("implement me")
}

// NewProfileStorageClient creates a new ProfileStorageClient
func NewSystemStorageClient(nameSpace string,
	logger *log.Logger,
	gatewayAddress, storageAddress, systemUserID, systemUserIDP, systemUserAPIKey string) SystemDataStorageClient {
	sdsci := SystemDataStorageClient{}
	logger.Debug().Msg("NewSystemStorageClient called")
	sdsc, err := metadata.NewCS3Storage(gatewayAddress, storageAddress, systemUserID, systemUserIDP, systemUserAPIKey)
	if err != nil {
		logger.Fatal().Err(err).Msg("could not create profile storage client")
	}
	sdsci.mds = &sdsc
	return sdsci
}
