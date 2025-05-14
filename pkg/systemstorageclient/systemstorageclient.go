package systemstorageclient

import (
	"context"
	"path"
	"sync"

	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata"
)

var (
	managerName = "systemdata"
)

type SystemDataStorageClient struct {
	mds metadata.Storage
	l   *sync.Mutex
}

func (s *SystemDataStorageClient) SimpleDownload(ctx context.Context, userID, identifier string) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (s *SystemDataStorageClient) SimpleUpload(ctx context.Context, userID, identifier string, content []byte) error {
	return s.mds.SimpleUpload(ctx, path.Join(userID, identifier), content)
}

func (s *SystemDataStorageClient) Delete(ctx context.Context, userID, identifier string) error {
	//TODO implement me
	panic("implement me")
}

func (s *SystemDataStorageClient) ReadDir(ctx context.Context, userID, identifier string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (s *SystemDataStorageClient) MakeDirIfNotExist(ctx context.Context, userID, identifier string) error {
	//TODO implement me
	panic("implement me")
}

// New initialize the store once, later calls are noops
func (s *SystemDataStorageClient) New(ctx context.Context,
	logger *log.Logger,
	scope, namespace string,
	gatewayAddress, storageAddress, systemUserID, systemUserIDP, systemUserAPIKey string,
) {
	if s.mds != nil {
		return
	}

	s.l.Lock()
	defer s.l.Unlock()

	if s.mds != nil {
		return
	}

	mds, err := metadata.NewCS3Storage(gatewayAddress, storageAddress, systemUserID, systemUserIDP, systemUserAPIKey)
	if err != nil {
		logger.Fatal().Err(err).Msg("could not create profile storage client")
	}
	s.mds = mds
}

// NewProfileStorageClient creates a new ProfileStorageClient
func NewSystemStorageClient(scope, nameSpace string,
	logger *log.Logger,
	gatewayAddress, storageAddress, systemUserID, systemUserIDP, systemUserAPIKey string) *SystemDataStorageClient {

	// scope: the scope the data should be persistet in (e.g. user)
	// namespace: the namespace the data should be persistet in (e.g. profilephoto)
	// results in the following path: /<scope>/*/<namespace>/*
	// e.g. /user/<uid>/profilephoto/profilephoto.jpg

	sdsci := &SystemDataStorageClient{}
	sdsci.New(context.TODO(), logger, scope, nameSpace, gatewayAddress, storageAddress, systemUserID, systemUserIDP, systemUserAPIKey)
	return sdsci
}
