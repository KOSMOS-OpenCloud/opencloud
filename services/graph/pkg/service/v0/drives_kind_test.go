package svc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	userprovider "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/go-chi/chi/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	libregraph "github.com/opencloud-eu/libre-graph-api-go"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/status"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	cs3mocks "github.com/opencloud-eu/reva/v2/tests/cs3mocks/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/tidwall/gjson"
	"google.golang.org/grpc"

	"github.com/opencloud-eu/opencloud/pkg/shared"
	v0 "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/messages/settings/v0"
	settingssvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/settings/v0"
	"github.com/opencloud-eu/opencloud/services/graph/mocks"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/config/defaults"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
	service "github.com/opencloud-eu/opencloud/services/graph/pkg/service/v0"
)

var _ = Describe("Drive kind", func() {
	var (
		graphService      service.Service
		gatewayClient     *cs3mocks.GatewayAPIClient
		permissionService mocks.Permissions
		eventsPublisher   mocks.Publisher
		ctx               context.Context
	)

	projectSpace := func(spaceID, spaceName, spaceKind string) *provider.StorageSpace {
		storageSpace := &provider.StorageSpace{
			Id:        &provider.StorageSpaceId{OpaqueId: "pro-1$" + spaceID},
			SpaceType: "project",
			Name:      spaceName,
			Root: &provider.ResourceId{
				StorageId: "pro-1",
				SpaceId:   spaceID,
				OpaqueId:  spaceID,
			},
		}
		if spaceKind != "" {
			storageSpace.Opaque = utils.AppendPlainToOpaque(nil, "spaceKind", spaceKind)
		}
		return storageSpace
	}

	listDrives := func(filterExpression string) *httptest.ResponseRecorder {
		target := "/graph/v1.0/me/drives"
		if filterExpression != "" {
			target += "?" + url.Values{"$filter": []string{filterExpression}}.Encode()
		}
		request := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
		recorder := httptest.NewRecorder()
		graphService.GetDrivesV1(recorder, request)
		return recorder
	}

	driveNames := func(recorder *httptest.ResponseRecorder) []string {
		var names []string
		for _, driveName := range gjson.Get(recorder.Body.String(), "value.#.name").Array() {
			names = append(names, driveName.String())
		}
		return names
	}

	BeforeEach(func() {
		ctx = revactx.ContextSetUser(context.Background(), &userprovider.User{Id: &userprovider.UserId{Type: userprovider.UserType_USER_TYPE_PRIMARY, OpaqueId: "testuser"}, Username: "testuser"})
		cfg := defaults.FullDefaultConfig()
		cfg.Identity.LDAP.CACert = "" // skip the startup checks, we don't use LDAP at all in this tests
		cfg.TokenManager.JWTSecret = "loremipsum"
		cfg.Commons = &shared.Commons{}
		cfg.GRPCClientTLS = &shared.GRPCClientTLS{}

		pool.RemoveSelector("GatewaySelector" + "eu.opencloud.api.gateway")
		gatewayClient = &cs3mocks.GatewayAPIClient{}
		gatewaySelector := pool.GetSelector[gateway.GatewayAPIClient](
			"GatewaySelector",
			"eu.opencloud.api.gateway",
			func(cc grpc.ClientConnInterface) gateway.GatewayAPIClient {
				return gatewayClient
			},
		)

		eventsPublisher = mocks.Publisher{}
		permissionService = mocks.Permissions{}
		permissionService.On("GetPermissionByID", mock.Anything, mock.Anything).Return(&settingssvc.GetPermissionByIDResponse{
			Permission: &v0.Permission{
				Operation:  v0.Permission_OPERATION_READWRITE,
				Constraint: v0.Permission_CONSTRAINT_ALL,
			},
		}, nil)

		gatewayClient.On("InitiateFileDownload", mock.Anything, mock.Anything).Return(&gateway.InitiateFileDownloadResponse{
			Status: status.NewNotFound(ctx, "not found"),
		}, nil)
		gatewayClient.On("GetQuota", mock.Anything, mock.Anything).Return(&provider.GetQuotaResponse{
			Status:     status.NewOK(ctx),
			TotalBytes: 500,
		}, nil)

		var err error
		graphService, err = service.NewService(
			service.Config(cfg),
			service.WithGatewaySelector(gatewaySelector),
			service.EventsPublisher(&eventsPublisher),
			service.PermissionService(&permissionService),
		)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("listing drives", func() {
		BeforeEach(func() {
			gatewayClient.On("ListStorageSpaces", mock.Anything, mock.Anything).Return(&provider.ListStorageSpacesResponse{
				Status: status.NewOK(ctx),
				StorageSpaces: []*provider.StorageSpace{
					projectSpace("plainID", "Plain project", ""),
					projectSpace("mailID", "Mailbox", "mail"),
					projectSpace("otherID", "Other kind", "other"),
				},
			}, nil)
		})

		It("returns the kind of a space as @libre.graph.kind and omits it for spaces without kind", func() {
			recorder := listDrives("")
			Expect(recorder.Code).To(Equal(http.StatusOK))

			kindByName := map[string]gjson.Result{}
			for _, drive := range gjson.Get(recorder.Body.String(), "value").Array() {
				kindByName[drive.Get("name").String()] = drive.Get(`@libre\.graph\.kind`)
			}
			Expect(kindByName).To(HaveLen(3))
			Expect(kindByName["Mailbox"].String()).To(Equal("mail"))
			Expect(kindByName["Other kind"].String()).To(Equal("other"))
			Expect(kindByName["Plain project"].Exists()).To(BeFalse())
		})

		DescribeTable("filters by kind",
			func(filterExpression string, expectedNames []string) {
				recorder := listDrives(filterExpression)
				Expect(recorder.Code).To(Equal(http.StatusOK))
				Expect(driveNames(recorder)).To(ConsistOf(expectedNames))
			},
			Entry("eq a kind", "@libre.graph.kind eq 'mail'", []string{"Mailbox"}),
			Entry("ne a kind", "@libre.graph.kind ne 'mail'", []string{"Plain project", "Other kind"}),
			Entry("eq null", "@libre.graph.kind eq null", []string{"Plain project"}),
			Entry("ne null", "@libre.graph.kind ne null", []string{"Mailbox", "Other kind"}),
			Entry("joined with driveType", "driveType eq 'project' and @libre.graph.kind eq 'mail'", []string{"Mailbox"}),
			Entry("joined with driveType, kind first", "@libre.graph.kind ne 'mail' and driveType eq 'project'", []string{"Plain project", "Other kind"}),
		)

		It("passes the driveType part of a joined filter to the storage provider", func() {
			recorder := listDrives("driveType eq 'project' and @libre.graph.kind eq 'mail'")
			Expect(recorder.Code).To(Equal(http.StatusOK))
			gatewayClient.AssertCalled(GinkgoT(), "ListStorageSpaces", mock.Anything, mock.MatchedBy(func(listRequest *provider.ListStorageSpacesRequest) bool {
				for _, filter := range listRequest.GetFilters() {
					if filter.GetType() == provider.ListStorageSpacesRequest_Filter_TYPE_SPACE_TYPE && filter.GetSpaceType() == "project" {
						return true
					}
				}
				return false
			}))
		})

		DescribeTable("rejects unsupported kind filters",
			func(filterExpression string) {
				recorder := listDrives(filterExpression)
				Expect(recorder.Code).To(Equal(http.StatusNotImplemented))
				var odataError libregraph.OdataError
				Expect(json.Unmarshal(recorder.Body.Bytes(), &odataError)).To(Succeed())
				Expect(odataError.Error.Code).To(Equal(errorcode.NotSupported.String()))
			},
			Entry("unsupported operand", "@libre.graph.kind gt 'mail'"),
			Entry("two kind conditions", "@libre.graph.kind eq 'mail' and @libre.graph.kind ne 'other'"),
		)
	})

	Describe("creating a drive", func() {
		It("passes the kind to the storage provider and returns it", func() {
			gatewayClient.On("CreateStorageSpace", mock.Anything, mock.MatchedBy(func(createRequest *provider.CreateStorageSpaceRequest) bool {
				return createRequest.GetType() == "project" && utils.ReadPlainFromOpaque(createRequest.GetOpaque(), "spaceKind") == "mail"
			})).Return(&provider.CreateStorageSpaceResponse{
				Status:       status.NewOK(ctx),
				StorageSpace: projectSpace("mailID", "Mailbox", "mail"),
			}, nil)

			requestBody := []byte(`{"name": "Mailbox", "driveType": "project", "@libre.graph.kind": "mail"}`)
			request := httptest.NewRequest(http.MethodPost, "/graph/v1.0/drives", bytes.NewBuffer(requestBody)).WithContext(ctx)
			recorder := httptest.NewRecorder()
			graphService.CreateDrive(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusCreated))

			Expect(gjson.Get(recorder.Body.String(), `@libre\.graph\.kind`).String()).To(Equal("mail"))
			Expect(gjson.Get(recorder.Body.String(), "driveType").String()).To(Equal("project"))
			gatewayClient.AssertNumberOfCalls(GinkgoT(), "CreateStorageSpace", 1)
		})

		It("does not pass an empty kind", func() {
			gatewayClient.On("CreateStorageSpace", mock.Anything, mock.MatchedBy(func(createRequest *provider.CreateStorageSpaceRequest) bool {
				_, hasKind := createRequest.GetOpaque().GetMap()["spaceKind"]
				return !hasKind
			})).Return(&provider.CreateStorageSpaceResponse{
				Status:       status.NewOK(ctx),
				StorageSpace: projectSpace("plainID", "Plain project", ""),
			}, nil)

			requestBody := []byte(`{"name": "Plain project", "@libre.graph.kind": ""}`)
			request := httptest.NewRequest(http.MethodPost, "/graph/v1.0/drives", bytes.NewBuffer(requestBody)).WithContext(ctx)
			recorder := httptest.NewRecorder()
			graphService.CreateDrive(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusCreated))
			Expect(gjson.Get(recorder.Body.String(), `@libre\.graph\.kind`).Exists()).To(BeFalse())
		})

		DescribeTable("rejects an invalid kind before calling the storage provider",
			func(requestBody string) {
				request := httptest.NewRequest(http.MethodPost, "/graph/v1.0/drives", bytes.NewBufferString(requestBody)).WithContext(ctx)
				recorder := httptest.NewRecorder()
				graphService.CreateDrive(recorder, request)
				Expect(recorder.Code).To(Equal(http.StatusBadRequest))
				gatewayClient.AssertNotCalled(GinkgoT(), "CreateStorageSpace", mock.Anything, mock.Anything)
			},
			Entry("upper case", `{"name": "Mailbox", "@libre.graph.kind": "Mail"}`),
			Entry("whitespace", `{"name": "Mailbox", "@libre.graph.kind": "mail box"}`),
			Entry("not a string", `{"name": "Mailbox", "@libre.graph.kind": 1}`),
		)

		It("still rejects other unknown properties", func() {
			requestBody := []byte(`{"name": "Mailbox", "@libre.graph.kind": "mail", "unknownProperty": true}`)
			request := httptest.NewRequest(http.MethodPost, "/graph/v1.0/drives", bytes.NewBuffer(requestBody)).WithContext(ctx)
			recorder := httptest.NewRecorder()
			graphService.CreateDrive(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
			gatewayClient.AssertNotCalled(GinkgoT(), "CreateStorageSpace", mock.Anything, mock.Anything)
		})
	})

	Describe("updating a drive", func() {
		updateRequest := func(requestBody string) *http.Request {
			request := httptest.NewRequest(http.MethodPatch, "/graph/v1.0/drives/{driveID}", bytes.NewBufferString(requestBody))
			routeContext := chi.NewRouteContext()
			routeContext.URLParams.Add("driveID", "pro-1$mailID")
			return request.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeContext))
		}

		It("passes the kind to the storage provider and returns it", func() {
			gatewayClient.On("UpdateStorageSpace", mock.Anything, mock.MatchedBy(func(updateSpaceRequest *provider.UpdateStorageSpaceRequest) bool {
				return utils.ReadPlainFromOpaque(updateSpaceRequest.GetStorageSpace().GetOpaque(), "spaceKind") == "mail"
			})).Return(&provider.UpdateStorageSpaceResponse{
				Status:       status.NewOK(ctx),
				StorageSpace: projectSpace("mailID", "Mailbox", "mail"),
			}, nil)

			recorder := httptest.NewRecorder()
			graphService.UpdateDrive(recorder, updateRequest(`{"@libre.graph.kind": "mail"}`))
			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(gjson.Get(recorder.Body.String(), `@libre\.graph\.kind`).String()).To(Equal("mail"))
			gatewayClient.AssertNumberOfCalls(GinkgoT(), "UpdateStorageSpace", 1)
		})

		It("rejects an invalid kind before calling the storage provider", func() {
			recorder := httptest.NewRecorder()
			graphService.UpdateDrive(recorder, updateRequest(`{"@libre.graph.kind": "-mail"}`))
			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
			gatewayClient.AssertNotCalled(GinkgoT(), "UpdateStorageSpace", mock.Anything, mock.Anything)
		})
	})

	Describe("getting a single drive", func() {
		It("returns the kind", func() {
			gatewayClient.On("ListStorageSpaces", mock.Anything, mock.Anything).Return(&provider.ListStorageSpacesResponse{
				Status:        status.NewOK(ctx),
				StorageSpaces: []*provider.StorageSpace{projectSpace("mailID", "Mailbox", "mail")},
			}, nil)

			request := httptest.NewRequest(http.MethodGet, "/graph/v1.0/drives/{driveID}/", nil)
			routeContext := chi.NewRouteContext()
			routeContext.URLParams.Add("driveID", "pro-1$mailID")
			request = request.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeContext))
			recorder := httptest.NewRecorder()
			graphService.GetSingleDrive(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusOK))

			body, _ := io.ReadAll(recorder.Body)
			Expect(gjson.GetBytes(body, `@libre\.graph\.kind`).String()).To(Equal("mail"))
			Expect(gjson.GetBytes(body, "name").String()).To(Equal("Mailbox"))
			Expect(gjson.GetBytes(body, "driveType").String()).To(Equal("project"))
		})
	})
})
