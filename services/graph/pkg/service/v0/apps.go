package svc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/go-chi/render"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
	"github.com/opencloud-eu/reva/v2/pkg/rhttp"
)

// AppConfig represents a .app/config.json from a space root.
type AppConfig struct {
	Name  string        `json:"name"`
	Icon  string        `json:"icon,omitempty"`
	Color string        `json:"color,omitempty"`
	Menu  []AppMenuItem `json:"menu,omitempty"`
}

// AppMenuItem represents a menu entry in the app config.
type AppMenuItem struct {
	Label    string        `json:"label"`
	Icon     string        `json:"icon,omitempty"`
	Path     string        `json:"path,omitempty"`
	URL      string        `json:"url,omitempty"`
	Children []AppMenuItem `json:"children,omitempty"`
}

// SpaceApp is the response for a single space app.
type SpaceApp struct {
	SpaceID    string        `json:"spaceId"`
	SpaceName  string        `json:"spaceName"`
	DriveAlias string        `json:"driveAlias"`
	DriveType  string        `json:"driveType"`
	Name       string        `json:"name"`
	Icon       string        `json:"icon,omitempty"`
	Color      string        `json:"color,omitempty"`
	Menu       []AppMenuItem `json:"menu,omitempty"`
}

// SpaceAppsResponse is the response for the apps endpoint.
type SpaceAppsResponse struct {
	Apps []SpaceApp `json:"apps"`
}

// GetSpaceApps returns all space apps the user has access to.
// GET /graph/v1.0/extensions/apps
func (g Graph) GetSpaceApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	gatewayClient, err := g.gatewaySelector.Next()
	if err != nil {
		g.logger.Error().Err(err).Msg("could not get gateway client")
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// List all spaces the user can access
	spacesRes, err := gatewayClient.ListStorageSpaces(ctx, &provider.ListStorageSpacesRequest{})
	if err != nil {
		g.logger.Error().Err(err).Msg("could not list storage spaces")
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if spacesRes.GetStatus().GetCode() != rpc.Code_CODE_OK {
		g.logger.Error().Str("status", spacesRes.GetStatus().GetMessage()).Msg("list spaces failed")
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusInternalServerError, spacesRes.GetStatus().GetMessage())
		return
	}

	var apps []SpaceApp

	for _, space := range spacesRes.GetStorageSpaces() {
		app := g.readSpaceApp(ctx, gatewayClient, space)
		if app != nil {
			apps = append(apps, *app)
		}
	}

	if apps == nil {
		apps = []SpaceApp{}
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, SpaceAppsResponse{Apps: apps})
}

// readSpaceApp reads .app/config.json from a space root and returns the app config.
func (g Graph) readSpaceApp(ctx context.Context, gw gateway.GatewayAPIClient, space *provider.StorageSpace) *SpaceApp {
	ref := &provider.Reference{
		ResourceId: space.GetRoot(),
		Path:       ".app/config.json",
	}

	// Stat to check if .app/config.json exists
	statRes, err := gw.Stat(ctx, &provider.StatRequest{Ref: ref})
	if err != nil || statRes.GetStatus().GetCode() != rpc.Code_CODE_OK {
		return nil
	}

	// Initiate download
	dlRes, err := gw.InitiateFileDownload(ctx, &provider.InitiateFileDownloadRequest{Ref: ref})
	if err != nil || dlRes.GetStatus().GetCode() != rpc.Code_CODE_OK {
		return nil
	}

	// Find the simple download protocol
	var downloadURL string
	for _, proto := range dlRes.GetProtocols() {
		if proto.GetProtocol() == "simple" || proto.GetProtocol() == "spaces" {
			downloadURL = proto.GetDownloadEndpoint()
			break
		}
	}
	if downloadURL == "" && len(dlRes.GetProtocols()) > 0 {
		downloadURL = dlRes.GetProtocols()[0].GetDownloadEndpoint()
	}
	if downloadURL == "" {
		return nil
	}

	// Download the file
	req, err := rhttp.NewRequest(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil
	}

	// Transfer token from download response
	for _, proto := range dlRes.GetProtocols() {
		if proto.GetToken() != "" {
			req.Header.Set("X-Reva-Transfer", proto.GetToken())
			break
		}
	}

	httpClient := rhttp.GetHTTPClient()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var config AppConfig
	if err := json.Unmarshal(body, &config); err != nil {
		g.logger.Warn().Err(err).Str("space", space.GetName()).Msg("invalid .app/config.json")
		return nil
	}

	// Extract drive alias from space opaque data
	driveAlias := ""
	driveType := ""
	if space.GetSpaceType() != "" {
		driveType = space.GetSpaceType()
	}
	if space.GetOpaque() != nil {
		if alias, ok := space.GetOpaque().GetMap()["spaceAlias"]; ok {
			driveAlias = string(alias.GetValue())
		}
	}

	return &SpaceApp{
		SpaceID:    space.GetId().GetOpaqueId(),
		SpaceName:  space.GetName(),
		DriveAlias: driveAlias,
		DriveType:  driveType,
		Name:       config.Name,
		Icon:       config.Icon,
		Color:      config.Color,
		Menu:       config.Menu,
	}
}
