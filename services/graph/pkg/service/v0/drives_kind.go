package svc

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"

	"github.com/CiscoM31/godata"
	storageprovider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	libregraph "github.com/opencloud-eu/libre-graph-api-go"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	"github.com/pkg/errors"
)

// The kind of a drive is a free classification of a space next to its type,
// e.g. "mail" for a project space that holds the messages of a mail account.
// libregraph.Drive has no field for it, so it travels as the extension
// property @libre.graph.kind in the drive JSON and as the opaque key spaceKind
// between graph and the storage provider.
const (
	// driveKindProperty is the JSON property and $filter name of the drive kind
	driveKindProperty = "@libre.graph.kind"
	// spaceKindOpaqueKey is the opaque key the storage provider reads and returns
	spaceKindOpaqueKey = "spaceKind"
)

// driveKindPattern matches the storage provider validation of a space kind
var driveKindPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// errInvalidDriveKind is returned for a drive kind that is not a short lower case identifier
var errInvalidDriveKind = errors.Errorf("invalid %s: must match %s", driveKindProperty, driveKindPattern.String())

// driveWithKind renders a libregraph drive with the kind of its space.
type driveWithKind struct {
	*libregraph.Drive
	Kind string
}

// MarshalJSON renders the libregraph drive and adds the kind property when the space has a kind.
func (drive driveWithKind) MarshalJSON() ([]byte, error) {
	if drive.Kind == "" {
		return json.Marshal(drive.Drive)
	}
	serializedDrive, err := drive.Drive.ToMap()
	if err != nil {
		return nil, err
	}
	serializedDrive[driveKindProperty] = drive.Kind
	return json.Marshal(serializedDrive)
}

// spaceKind returns the kind of a storage space, empty if it has none.
func spaceKind(space *storageprovider.StorageSpace) string {
	return utils.ReadPlainFromOpaque(space.GetOpaque(), spaceKindOpaqueKey)
}

// spaceKindKey identifies a space by storage and space id, the same way for a
// storage space and for the drive formatted from it.
func spaceKindKey(storageID, spaceID string) string {
	return storageID + "$" + spaceID
}

// withDriveKinds pairs the formatted drives with the kinds of the storage spaces they were made from.
func withDriveKinds(drives []*libregraph.Drive, storageSpaces []*storageprovider.StorageSpace) []driveWithKind {
	if drives == nil {
		return nil
	}
	kindBySpace := make(map[string]string, len(storageSpaces))
	for _, storageSpace := range storageSpaces {
		if kind := spaceKind(storageSpace); kind != "" {
			spaceRoot := storageSpace.GetRoot()
			kindBySpace[spaceKindKey(spaceRoot.GetStorageId(), spaceRoot.GetSpaceId())] = kind
		}
	}
	drivesWithKind := make([]driveWithKind, 0, len(drives))
	for _, drive := range drives {
		renderedDrive := driveWithKind{Drive: drive}
		if driveID, err := storagespace.ParseID(drive.GetId()); err == nil {
			renderedDrive.Kind = kindBySpace[spaceKindKey(driveID.GetStorageId(), driveID.GetSpaceId())]
		}
		drivesWithKind = append(drivesWithKind, renderedDrive)
	}
	return drivesWithKind
}

// extractDriveKind takes the kind property out of a drive request body, so the
// rest can be decoded strictly into a libregraph type. A missing or null kind
// returns nil, an empty kind returns a pointer to the empty string.
func extractDriveKind(body io.Reader) (io.Reader, *string, error) {
	rawBody, err := io.ReadAll(body)
	if err != nil {
		return nil, nil, err
	}
	var bodyProperties map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &bodyProperties); err != nil {
		// not an object, leave the error message to the strict decoder
		return bytes.NewReader(rawBody), nil, nil
	}
	rawKind, hasKind := bodyProperties[driveKindProperty]
	if !hasKind {
		return bytes.NewReader(rawBody), nil, nil
	}
	delete(bodyProperties, driveKindProperty)
	remainingBody, err := json.Marshal(bodyProperties)
	if err != nil {
		return nil, nil, err
	}

	var driveKind *string
	if err := json.Unmarshal(rawKind, &driveKind); err != nil {
		return nil, nil, errInvalidDriveKind
	}
	if driveKind != nil && *driveKind != "" && !driveKindPattern.MatchString(*driveKind) {
		return nil, nil, errInvalidDriveKind
	}
	return bytes.NewReader(remainingBody), driveKind, nil
}

// driveKindFilter is a $filter condition on the drive kind.
// An empty kind stands for null, i.e. a space without kind.
type driveKindFilter struct {
	kind   string
	negate bool
}

// matches reports whether a storage space passes the kind condition.
func (filter *driveKindFilter) matches(space *storageprovider.StorageSpace) bool {
	if filter == nil {
		return true
	}
	return (spaceKind(space) == filter.kind) != filter.negate
}

// filterSpacesByKind keeps the storage spaces that pass the kind condition.
func filterSpacesByKind(storageSpaces []*storageprovider.StorageSpace, filter *driveKindFilter) []*storageprovider.StorageSpace {
	if filter == nil {
		return storageSpaces
	}
	matchingSpaces := make([]*storageprovider.StorageSpace, 0, len(storageSpaces))
	for _, storageSpace := range storageSpaces {
		if filter.matches(storageSpace) {
			matchingSpaces = append(matchingSpaces, storageSpace)
		}
	}
	return matchingSpaces
}

// extractDriveKindFilter takes a kind condition out of the $filter of the request.
// Supported are "@libre.graph.kind eq|ne 'value'|null" on its own or joined by
// "and" with one condition the storage provider understands (e.g. driveType eq 'project').
// The kind condition is removed from the request, the rest is left to generateCs3Filters.
func extractDriveKindFilter(request *godata.GoDataRequest) (*driveKindFilter, error) {
	if request.Query.Filter == nil || request.Query.Filter.Tree == nil {
		return nil, nil
	}
	filterTree := request.Query.Filter.Tree

	if isDriveKindCondition(filterTree) {
		kindFilter, err := parseDriveKindCondition(filterTree)
		if err != nil {
			return nil, err
		}
		request.Query.Filter = nil
		return kindFilter, nil
	}

	if filterTree.Token.Value != "and" || len(filterTree.Children) != 2 {
		return nil, nil
	}
	leftCondition, rightCondition := filterTree.Children[0], filterTree.Children[1]
	switch {
	case isDriveKindCondition(leftCondition) && isDriveKindCondition(rightCondition):
		return nil, errors.Errorf("only one condition on %s is supported", driveKindProperty)
	case isDriveKindCondition(leftCondition):
		kindFilter, err := parseDriveKindCondition(leftCondition)
		if err != nil {
			return nil, err
		}
		request.Query.Filter.Tree = rightCondition
		return kindFilter, nil
	case isDriveKindCondition(rightCondition):
		kindFilter, err := parseDriveKindCondition(rightCondition)
		if err != nil {
			return nil, err
		}
		request.Query.Filter.Tree = leftCondition
		return kindFilter, nil
	}
	return nil, nil
}

// isDriveKindCondition reports whether a filter node is a comparison on the drive kind.
func isDriveKindCondition(node *godata.ParseNode) bool {
	if node == nil || len(node.Children) != 2 {
		return false
	}
	return node.Children[0].Token.Value == driveKindProperty
}

// parseDriveKindCondition turns "@libre.graph.kind eq|ne 'value'|null" into a kind filter.
func parseDriveKindCondition(node *godata.ParseNode) (*driveKindFilter, error) {
	kindFilter := &driveKindFilter{}
	switch node.Token.Value {
	case "eq":
	case "ne":
		kindFilter.negate = true
	default:
		return nil, errors.Errorf("unsupported filter operand on %s: %s", driveKindProperty, node.Token.Value)
	}
	valueToken := node.Children[1].Token
	switch valueToken.Type {
	case godata.ExpressionTokenNull:
		kindFilter.kind = ""
	case godata.ExpressionTokenString:
		kindFilter.kind = strings.Trim(valueToken.Value, "'")
	default:
		return nil, errors.Errorf("unsupported filter value on %s: %s", driveKindProperty, valueToken.Value)
	}
	return kindFilter, nil
}
