package svc

import (
	"fmt"
	"github.com/CiscoM31/godata"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	"github.com/go-chi/render"
	libregraph "github.com/opencloud-eu/libre-graph-api-go"
	"github.com/opencloud-eu/opencloud/services/graph/pkg/odata"
	"github.com/opencloud-eu/opencloud/services/thumbnails/pkg/thumbnail"
	"github.com/opencloud-eu/reva/v2/pkg/signedurl"
	"net/http"
	"slices"
	"time"

	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
)

type driveItemsByResourceID map[string]libregraph.DriveItem

// parseShareByMeRequest parses the odata request and returns the parsed request and a boolean indicating if the request should expand thumbnails.
func parseShareByMeRequest(r *http.Request) (*godata.GoDataRequest, bool, error) {
	odataReq, err := godata.ParseRequest(r.Context(), sanitizePath(r.URL.Path, APIVersion_1), r.URL.Query())
	if err != nil {
		return nil, false, errorcode.New(errorcode.InvalidRequest, err.Error())
	}
	exp, err := odata.GetExpandValues(odataReq.Query)
	if err != nil {
		return nil, false, errorcode.New(errorcode.InvalidRequest, err.Error())
	}
	expandThumbnails := slices.Contains(exp, "thumbnails")
	return odataReq, expandThumbnails, nil
}

// GetSharedByMe implements the Service interface (/me/drives/sharedByMe endpoint)
func (g Graph) GetSharedByMe(w http.ResponseWriter, r *http.Request) {
	g.logger.Debug().Msg("Calling GetRootDriveChildren")
	ctx := r.Context()

	_, expandThumbnails, err := parseShareByMeRequest(r)
	if err != nil {
		errorcode.RenderError(w, r, err)
		return
	}
	fmt.Println("expandThumbnails:", expandThumbnails)

	driveItems := make(driveItemsByResourceID)
	driveItems, err = g.listUserShares(ctx, nil, driveItems)
	if err != nil {
		errorcode.RenderError(w, r, err)
		return
	}

	if g.config.IncludeOCMSharees {
		driveItems, err = g.listOCMShares(ctx, nil, driveItems)
		if err != nil {
			errorcode.RenderError(w, r, err)
			return
		}
	}

	driveItems, err = g.listPublicShares(ctx, nil, driveItems)
	if err != nil {
		errorcode.RenderError(w, r, err)
		return
	}

	if expandThumbnails {
		for k, item := range driveItems {
			mt := item.GetFile().MimeType
			if mt == nil {
				continue
			}

			signer, err := signedurl.NewJWTSignedURL(signedurl.WithSecret("abcde"))
			if err != nil {
				panic("failed to create signer")
			}

			_, match := thumbnail.SupportedMimeTypes[*mt]
			if match {
				signedURL, err := signer.Sign("https://localhost:9200/", item.GetId(), 30*time.Minute)
				if err != nil {
					g.logger.Error().Err(err).Msg("Failed to get thumbnail URL")
					continue
				}
				t := libregraph.NewThumbnail()
				t.SetUrl(signedURL)
				item.SetThumbnails([]libregraph.ThumbnailSet{
					{
						Small:  t,
						Medium: t,
						Large:  t,
					},
				})

				driveItems[k] = item // assign modified item back to the map
			}
		}

	}

	res := make([]libregraph.DriveItem, 0, len(driveItems))
	for _, v := range driveItems {
		res = append(res, v)
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, &ListResponse{Value: res})
}

func cs3StatusToErrCode(code rpc.Code) (errcode errorcode.ErrorCode) {
	switch code {
	case rpc.Code_CODE_UNAUTHENTICATED:
		errcode = errorcode.Unauthenticated
	case rpc.Code_CODE_PERMISSION_DENIED:
		errcode = errorcode.AccessDenied
	case rpc.Code_CODE_NOT_FOUND:
		errcode = errorcode.ItemNotFound
	case rpc.Code_CODE_LOCKED:
		errcode = errorcode.ItemIsLocked
	case rpc.Code_CODE_INVALID_ARGUMENT:
		errcode = errorcode.InvalidRequest
	case rpc.Code_CODE_FAILED_PRECONDITION:
		errcode = errorcode.InvalidRequest
	default:
		errcode = errorcode.GeneralException
	}
	return errcode
}
