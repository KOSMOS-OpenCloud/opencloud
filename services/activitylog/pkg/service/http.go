package service

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/events"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	"google.golang.org/grpc/metadata"

	libregraph "github.com/opencloud-eu/libre-graph-api-go"
	"github.com/opencloud-eu/opencloud/pkg/ast"
	"github.com/opencloud-eu/opencloud/pkg/kql"
	"github.com/opencloud-eu/opencloud/pkg/l10n"
	ehmsg "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/messages/eventhistory/v0"
	ehsvc "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/services/eventhistory/v0"
)

var (
	//go:embed l10n/locale
	_localeFS embed.FS

	// subfolder where the translation files are stored
	_localeSubPath = "l10n/locale"

	// domain of the activitylog service (transifex)
	_domain = "activitylog"
)

// ServeHTTP implements the http.Handler interface.
func (s *ActivitylogService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// HandleGetItemActivities handles the request to get the activities of an item.
func (s *ActivitylogService) HandleGetItemActivities(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx = metadata.AppendToOutgoingContext(ctx, revactx.TokenHeader, r.Header.Get(revactx.TokenHeader))

	activeUser, ok := revactx.ContextGetUser(ctx)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	gwc, err := s.gws.Next()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	filters, err := s.getFilters(r.URL.Query().Get("kql"))
	if err != nil {
		s.log.Info().Str("query", r.URL.Query().Get("kql")).Err(err).Msg("error getting filters")
		_, _ = w.Write([]byte(err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	rid := filters.rid
	info, err := utils.GetResourceByID(ctx, rid, gwc)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// you need ListGrants to see activities
	if !info.GetPermissionSet().GetListGrants() {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	raw, err := s.Activities(rid)
	if err != nil {
		s.log.Error().Err(err).Msg("error getting activities")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	ids := make([]string, 0, len(raw))
	toDelete := make(map[string]struct{}, len(raw))
	for _, a := range raw {
		if !filters.rawFilter(a) {
			continue
		}
		ids = append(ids, a.EventID)
		toDelete[a.EventID] = struct{}{}
	}

	evRes, err := s.evHistory.GetEvents(r.Context(), &ehsvc.GetEventsRequest{Ids: ids})
	if err != nil {
		s.log.Error().Err(err).Msg("error getting events")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	evs := evRes.GetEvents()
	filters.sortFunc(evs)

	loc := l10n.MustGetUserLocale(r.Context(), activeUser.GetId().GetOpaqueId(), r.Header.Get(l10n.HeaderAcceptLanguage), s.valService)
	t := l10n.NewTranslatorFromCommonConfig(s.cfg.DefaultLanguage, _domain, s.cfg.TranslationPath, _localeFS, _localeSubPath)

	activities := make([]libregraph.Activity, 0, len(evs))
	for _, e := range evs {
		delete(toDelete, e.GetId())

		if filters.limit > 0 && filters.limit <= len(activities) {
			continue
		}

		if !filters.eventFilter(e) {
			continue
		}

		var (
			message string
			ts      time.Time
			vars    map[string]any
		)

		switch ev := s.unwrapEvent(e).(type) {
		case nil:
			// error already logged in unwrapEvent
			continue
		case events.UploadReady:
			message = MessageResourceCreated
			if ev.IsVersion {
				message = MessageResourceUpdated
			}
			ts = utils.TSToTime(ev.Timestamp)
			vars, err = s.GetVars(ctx, WithResource(ev.FileRef, false, ""), WithUser(nil, ev.ExecutingUser, ev.ImpersonatingUser))
		case events.FileTouched:
			message = MessageResourceCreated
			ts = utils.TSToTime(ev.Timestamp)
			vars, err = s.GetVars(ctx, WithResource(ev.Ref, false, ""), WithUser(ev.Executant, nil, ev.ImpersonatingUser))
		case events.FileDownloaded:
			message = MessageResourceDownloaded
			ts = utils.TSToTime(ev.Timestamp)
			vars, err = s.GetVars(ctx, WithResource(ev.Ref, false, ""), WithUser(ev.Executant, nil, ev.ImpersonatingUser), WithVar("token", "", ev.ImpersonatingUser.GetId().GetOpaqueId()))
		case events.ContainerCreated:
			message = MessageResourceCreated
			ts = utils.TSToTime(ev.Timestamp)
			vars, err = s.GetVars(ctx, WithResource(ev.Ref, false, ""), WithUser(ev.Executant, nil, ev.ImpersonatingUser))
		case events.ItemTrashed:
			message = MessageResourceTrashed
			ts = utils.TSToTime(ev.Timestamp)
			vars, err = s.GetVars(ctx, WithTrashedResource(ev.Ref, ev.ID), WithUser(ev.Executant, nil, ev.ImpersonatingUser))
		case events.ItemMoved:
			switch isRename(ev.OldReference, ev.Ref) {
			case true:
				message = MessageResourceRenamed
				vars, err = s.GetVars(ctx, WithResource(ev.Ref, false, ""), WithOldResource(ev.OldReference), WithUser(ev.Executant, nil, ev.ImpersonatingUser))
			case false:
				message = MessageResourceMoved
				vars, err = s.GetVars(ctx, WithResource(ev.Ref, false, ""), WithUser(ev.Executant, nil, ev.ImpersonatingUser))
			}
			ts = utils.TSToTime(ev.Timestamp)
		case events.ShareCreated:
			message = MessageShareCreated
			ts = utils.TSToTime(ev.CTime)
			vars, err = s.GetVars(ctx,
				WithResource(toRef(ev.ItemID), false, ev.ResourceName),
				WithUser(ev.Executant, nil, nil),
				WithSharee(ev.GranteeUserID, ev.GranteeGroupID))
		case events.ShareUpdated:
			if ev.Sharer != nil && ev.ItemID != nil && ev.Sharer.GetOpaqueId() == ev.ItemID.GetSpaceId() {
				continue
			}
			message = MessageShareUpdated
			ts = utils.TSToTime(ev.MTime)
			vars, err = s.GetVars(ctx,
				WithResource(toRef(ev.ItemID), false, ev.ResourceName),
				WithUser(ev.Executant, nil, nil),
				WithTranslation(&t, loc, "field", ev.UpdateMask))
		case events.ShareRemoved:
			message = MessageShareDeleted
			ts = ev.Timestamp
			vars, err = s.GetVars(ctx,
				WithResource(toRef(ev.ItemID), false, ev.ResourceName),
				WithUser(ev.Executant, nil, nil),
				WithSharee(ev.GranteeUserID, ev.GranteeGroupID))
		case events.LinkCreated:
			message = MessageLinkCreated
			ts = utils.TSToTime(ev.CTime)
			vars, err = s.GetVars(ctx,
				WithResource(toRef(ev.ItemID), false, ev.ResourceName),
				WithUser(ev.Executant, nil, nil))
		case events.LinkUpdated:
			if ev.Sharer != nil && ev.ItemID != nil && ev.Sharer.GetOpaqueId() == ev.ItemID.GetSpaceId() {
				continue
			}
			message = MessageLinkUpdated
			ts = utils.TSToTime(ev.MTime)
			vars, err = s.GetVars(ctx,
				WithVar("resource", storagespace.FormatResourceID(ev.ItemID), ev.ResourceName),
				WithUser(ev.Executant, nil, nil),
				WithTranslation(&t, loc, "field", []string{ev.FieldUpdated}),
				WithVar("token", ev.ItemID.GetOpaqueId(), ev.DisplayName))
		case events.LinkRemoved:
			message = MessageLinkDeleted
			ts = utils.TSToTime(ev.Timestamp)
			vars, err = s.GetVars(ctx, WithResource(toRef(ev.ItemID), false, ""), WithUser(ev.Executant, nil, nil))
		case events.SpaceShared:
			message = MessageSpaceShared
			ts = ev.Timestamp
			vars, err = s.GetVars(ctx, WithSpace(ev.ID), WithUser(ev.Executant, nil, nil), WithSharee(ev.GranteeUserID, ev.GranteeGroupID))
		case events.SpaceUnshared:
			message = MessageSpaceUnshared
			ts = ev.Timestamp
			vars, err = s.GetVars(ctx, WithSpace(ev.ID), WithUser(ev.Executant, nil, nil), WithSharee(ev.GranteeUserID, ev.GranteeGroupID))
		}

		if err != nil {
			s.log.Error().Err(err).Msg("error getting response data")
			continue
		}

		activities = append(activities, NewActivity(t.Translate(message, loc), ts, e.GetId(), vars))
	}

	// delete activities in separate go routine
	if len(toDelete) > 0 {
		go func() {
			err := s.RemoveActivities(rid, toDelete)
			if err != nil {
				s.log.Error().Err(err).Msg("error removing activities")
			}
		}()
	}

	// Build response: grouped or flat
	var responseBody any
	if filters.groupBy != "" {
		responseBody = s.groupActivities(activities, filters.groupBy)
	} else {
		responseBody = GetActivitiesResponse{Activities: activities}
	}

	b, err := json.Marshal(responseBody)
	if err != nil {
		s.log.Error().Err(err).Msg("error marshalling activities")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if _, err := w.Write(b); err != nil {
		s.log.Error().Err(err).Msg("error writing response")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *ActivitylogService) unwrapEvent(e *ehmsg.Event) any {
	etype, ok := s.registeredEvents[e.GetType()]
	if !ok {
		s.log.Error().Str("eventid", e.GetId()).Str("eventtype", e.GetType()).Msg("event not registered")
		return nil
	}

	einterface, err := etype.Unmarshal(e.GetEvent())
	if err != nil {
		s.log.Error().Str("eventid", e.GetId()).Str("eventtype", e.GetType()).Msg("failed to umarshal event")
		return nil
	}

	return einterface
}

type filterResult struct {
	rid              *provider.ResourceId
	limit            int
	groupBy          string // "", "user", "container"
	rawFilter        func(RawActivity) bool
	eventFilter      func(*ehmsg.Event) bool
	sortFunc         func([]*ehmsg.Event)
}

func (s *ActivitylogService) getFilters(query string) (*filterResult, error) {
	qast, err := kql.Builder{}.Build(query)
	if err != nil {
		return nil, err
	}

	prefilters := make([]func(RawActivity) bool, 0)
	postfilters := make([]func(*ehmsg.Event) bool, 0)

	sortby := func(_ []*ehmsg.Event) {}

	var (
		itemID  string
		limit   int
		groupBy string
	)

	for _, n := range qast.Nodes {
		switch v := n.(type) {
		case *ast.StringNode:
			switch strings.ToLower(v.Key) {
			case "itemid":
				itemID = v.Value
			case "depth":
				depth, err := strconv.Atoi(v.Value)
				if err != nil {
					return nil, err
				}
				if depth == -1 {
					break
				}

				prefilters = append(prefilters, func(a RawActivity) bool {
					return a.Depth <= depth
				})
			case "limit":
				l, err := strconv.Atoi(v.Value)
				if err != nil {
					return nil, err
				}

				limit = l
			case "sort":
				switch v.Value {
				case "asc":
					// nothing to do - already ascending
				case "desc":
					sortby = func(activities []*ehmsg.Event) {
						slices.Reverse(activities)
					}
				}
			case "timerange":
				cutoff, err := parseTimeRange(v.Value)
				if err != nil {
					return nil, err
				}
				prefilters = append(prefilters, func(a RawActivity) bool {
					return a.Timestamp.After(cutoff)
				})
			case "groupby":
				switch v.Value {
				case "user", "container":
					groupBy = v.Value
				default:
					return nil, fmt.Errorf("unsupported groupby value: %s (use 'user' or 'container')", v.Value)
				}
			}
		case *ast.DateTimeNode:
			switch v.Operator.Value {
			case "<", "<=":
				prefilters = append(prefilters, func(a RawActivity) bool {
					return a.Timestamp.Before(v.Value)
				})
			case ">", ">=":
				prefilters = append(prefilters, func(a RawActivity) bool {
					return a.Timestamp.After(v.Value)
				})
			}
		case *ast.OperatorNode:
			if v.Value != "AND" {
				return nil, errors.New("only AND operator is supported")
			}
		}
	}

	rid, err := storagespace.ParseID(itemID)
	if err != nil {
		return nil, err
	}
	if rid.GetOpaqueId() == "" {
		// space root requested - fix format
		rid.OpaqueId = rid.GetSpaceId()
	}
	pref := func(a RawActivity) bool {
		for _, f := range prefilters {
			if !f(a) {
				return false
			}
		}
		return true
	}
	postf := func(e *ehmsg.Event) bool {
		for _, f := range postfilters {
			if !f(e) {
				return false
			}
		}
		return true
	}
	return &filterResult{
		rid:         &rid,
		limit:       limit,
		groupBy:     groupBy,
		rawFilter:   pref,
		eventFilter: postf,
		sortFunc:    sortby,
	}, nil
}

// parseTimeRange converts shorthand time ranges to a cutoff time
func parseTimeRange(val string) (time.Time, error) {
	now := time.Now()
	switch val {
	case "7d":
		return now.AddDate(0, 0, -7), nil
	case "1m":
		return now.AddDate(0, -1, 0), nil
	case "3m":
		return now.AddDate(0, -3, 0), nil
	case "6m":
		return now.AddDate(0, -6, 0), nil
	case "1y":
		return now.AddDate(-1, 0, 0), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported timerange: %s (use 7d, 1m, 3m, 6m, 1y)", val)
	}
}

// groupActivities groups activities by user or container based on their template variables
func (s *ActivitylogService) groupActivities(activities []libregraph.Activity, groupBy string) GroupedActivitiesResponse {
	grouped := make(map[string]*ActivityGroup)
	var order []string

	for _, a := range activities {
		var key, label string
		vars := a.Template.Variables

		switch groupBy {
		case "user":
			if user, ok := vars["user"]; ok {
				if userMap, ok := user.(map[string]any); ok {
					key = fmt.Sprintf("user:%v", userMap["id"])
					label = fmt.Sprintf("%v", userMap["displayName"])
				}
			}
		case "container":
			if res, ok := vars["resource"]; ok {
				if resMap, ok := res.(map[string]any); ok {
					key = fmt.Sprintf("resource:%v", resMap["id"])
					label = fmt.Sprintf("%v", resMap["name"])
				}
			}
		}

		if key == "" {
			key = "other"
			label = "Other"
		}

		if _, exists := grouped[key]; !exists {
			grouped[key] = &ActivityGroup{Key: key, Label: label}
			order = append(order, key)
		}
		g := grouped[key]
		g.Activities = append(g.Activities, a)
		g.Count = len(g.Activities)
	}

	groups := make([]ActivityGroup, 0, len(order))
	for _, k := range order {
		groups = append(groups, *grouped[k])
	}

	return GroupedActivitiesResponse{
		GroupBy: groupBy,
		Groups:  groups,
	}
}

// returns true if this is just a rename
func isRename(o, n *provider.Reference) bool {
	// if resourceids are different we assume it is a move
	if !utils.ResourceIDEqual(o.GetResourceId(), n.GetResourceId()) {
		return false
	}
	return filepath.Base(o.GetPath()) != filepath.Base(n.GetPath())
}
