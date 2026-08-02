package bleve

import (
	"reflect"
	"regexp"
	"strings"
	"time"

	bleveSearch "github.com/blevesearch/bleve/v2/search"
	storageProvider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	libregraph "github.com/opencloud-eu/libre-graph-api-go"
	"google.golang.org/protobuf/types/known/timestamppb"

	searchMessage "github.com/opencloud-eu/opencloud/protogen/gen/opencloud/messages/search/v0"
	"github.com/opencloud-eu/opencloud/services/search/pkg/content"
	"github.com/opencloud-eu/opencloud/services/search/pkg/search"
)

var queryEscape = regexp.MustCompile(`([` + regexp.QuoteMeta(`+=&|><!(){}[]^\"~*?:\/`) + `\-\s])`)

func getFieldValue[T any](m map[string]any, key string) (out T) {
	val, ok := m[key]
	if !ok {
		return
	}

	out, _ = val.(T)

	return
}

func resourceIDtoSearchID(id storageProvider.ResourceId) *searchMessage.ResourceID {
	return &searchMessage.ResourceID{
		StorageId: id.GetStorageId(),
		SpaceId:   id.GetSpaceId(),
		OpaqueId:  id.GetOpaqueId()}
}

func getFieldSliceValue[T any](m map[string]any, key string) (out []T) {
	iv := getFieldValue[any](m, key)
	add := func(v any) {
		cv, ok := v.(T)
		if !ok {
			return
		}

		out = append(out, cv)
	}

	// bleve tend to convert []string{"foo"} to type string if slice contains only one value
	// bleve: []string{"foo"} -> "foo"
	// bleve: []string{"foo", "bar"} -> []string{"foo", "bar"}
	switch v := iv.(type) {
	case T:
		add(v)
	case []any:
		for _, rv := range v {
			add(rv)
		}
	}

	return
}

// buildHighlights combines Content and Metadata highlights into a single string.
// Content highlights are shown first, followed by metadata field matches.
// friendlyKey maps internal metadata keys to human-readable labels.
var friendlyKey = map[string]string{
	"oy.fileReference": "Aktenzeichen",
	"oy.subject":       "Betreff",
	"oy.fullPath":      "Pfad",
	"doc.subject":      "Dokument",
	"doc.type":         "Typ",
	"sender.company":   "Absender",
	"sender.email":     "E-Mail",
	"info.subject":     "Info",
	"note":             "Notiz",
}

func buildHighlights(fragments bleveSearch.FieldFragmentMap, fields map[string]interface{}, query string) string {
	searchTerm := extractSearchTerm(query)
	var parts []string

	// Content highlight (traditional full-text match)
	if contentFrags, ok := fragments["Content"]; ok && len(contentFrags) > 0 {
		parts = append(parts, contentFrags[0])
	}

	// Metadata highlights from bleve fragments
	for field, frags := range fragments {
		if strings.HasPrefix(field, "Metadata.") && len(frags) > 0 {
			key := strings.TrimPrefix(field, "Metadata.")
			parts = append(parts, formatHighlight(key, frags[0], searchTerm))
		}
	}

	// Fallback: if bleve didn't produce fragments (e.g. wildcard queries),
	// check metadata fields manually for the search term
	if len(parts) == 0 && searchTerm != "" {
		lowerTerm := strings.ToLower(searchTerm)
		for k, v := range fields {
			if !strings.HasPrefix(strings.ToLower(k), "metadata.") {
				continue
			}
			if s, ok := v.(string); ok && strings.Contains(strings.ToLower(s), lowerTerm) {
				key := k[len("Metadata."):]
				// lowercase variant
				if strings.HasPrefix(k, "metadata.") {
					key = k[len("metadata."):]
				}
				parts = append(parts, formatHighlight(key, s, searchTerm))
			}
		}
	}

	// Limit to 3 most relevant highlights
	if len(parts) > 3 {
		parts = parts[:3]
	}

	return strings.Join(parts, " · ")
}

// formatHighlight formats a single highlight entry with friendly key name,
// truncated value, and <mark> tags around the search term.
func formatHighlight(key, value, searchTerm string) string {
	// Use friendly name if available
	label := key
	if friendly, ok := friendlyKey[key]; ok {
		label = friendly
	}

	// Truncate long values around the match
	value = truncateAroundMatch(value, searchTerm, 80)

	// Mark the search term in the value
	if searchTerm != "" {
		value = markTerm(value, searchTerm)
	}

	return "<b>" + label + ":</b> " + value
}

// truncateAroundMatch truncates a string to maxLen chars, centered on the first
// occurrence of term. Adds "..." where truncated.
func truncateAroundMatch(s, term string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	idx := strings.Index(strings.ToLower(s), strings.ToLower(term))
	if idx < 0 {
		return s[:maxLen] + "..."
	}
	start := idx - maxLen/2
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(s) {
		end = len(s)
		start = end - maxLen
		if start < 0 {
			start = 0
		}
	}
	result := s[start:end]
	if start > 0 {
		result = "..." + result
	}
	if end < len(s) {
		result = result + "..."
	}
	return result
}

// markTerm wraps occurrences of term in <mark> tags (case-insensitive).
func markTerm(s, term string) string {
	lower := strings.ToLower(s)
	lowerTerm := strings.ToLower(term)
	var result strings.Builder
	pos := 0
	for {
		idx := strings.Index(lower[pos:], lowerTerm)
		if idx < 0 {
			result.WriteString(s[pos:])
			break
		}
		result.WriteString(s[pos : pos+idx])
		result.WriteString("<mark>")
		result.WriteString(s[pos+idx : pos+idx+len(term)])
		result.WriteString("</mark>")
		pos += idx + len(term)
	}
	return result.String()
}

// extractSearchTerm pulls the raw search term from a query like name:"*Sparkasse*"
func extractSearchTerm(query string) string {
	if i := strings.Index(query, `name:"`); i >= 0 {
		rest := query[i+6:]
		if j := strings.Index(rest, `"`); j >= 0 {
			term := rest[:j]
			term = strings.Trim(term, "*")
			return term
		}
	}
	return ""
}

func getFragmentValue(m bleveSearch.FieldFragmentMap, key string, idx int) string {
	val, ok := m[key]
	if !ok {
		return ""
	}

	if len(val) <= idx {
		return ""
	}

	return val[idx]
}

func getAudioValue[T any](fields map[string]any) *T {
	if !strings.HasPrefix(getFieldValue[string](fields, "MimeType"), "audio/") {
		return nil
	}

	var audio = newPointerOfType[T]()
	if ok := unmarshalInterfaceMap(audio, fields, "audio."); ok {
		return audio
	}

	return nil
}

func getImageValue[T any](fields map[string]any) *T {
	var image = newPointerOfType[T]()
	if ok := unmarshalInterfaceMap(image, fields, "image."); ok {
		return image
	}

	return nil
}

func getLocationValue[T any](fields map[string]any) *T {
	var location = newPointerOfType[T]()
	if ok := unmarshalInterfaceMap(location, fields, "location."); ok {
		return location
	}

	return nil
}

func getPhotoValue[T any](fields map[string]any) *T {
	var photo = newPointerOfType[T]()
	if ok := unmarshalInterfaceMap(photo, fields, "photo."); ok {
		return photo
	}

	return nil
}

func newPointerOfType[T any]() *T {
	t := reflect.TypeOf((*T)(nil)).Elem()
	ptr := reflect.New(t).Interface()
	return ptr.(*T)
}

func unmarshalInterfaceMap(out any, flatMap map[string]any, prefix string) bool {
	nonEmpty := false
	obj := reflect.ValueOf(out).Elem()
	for i := 0; i < obj.NumField(); i++ {
		field := obj.Field(i)
		structField := obj.Type().Field(i)
		mapKey := prefix + getFieldName(structField)

		if value, ok := flatMap[mapKey]; ok {
			if field.Kind() == reflect.Ptr {
				alloc := reflect.New(field.Type().Elem())
				elemType := field.Type().Elem()

				// convert time strings from index for search requests
				if elemType == reflect.TypeOf(timestamppb.Timestamp{}) {
					if strValue, ok := value.(string); ok {
						if parsedTime, err := time.Parse(time.RFC3339, strValue); err == nil {
							alloc.Elem().Set(reflect.ValueOf(*timestamppb.New(parsedTime)))
							field.Set(alloc)
							nonEmpty = true
						}
					}
					continue
				}

				// convert time strings from index for libregraph structs when updating resources
				if elemType == reflect.TypeOf(time.Time{}) {
					if strValue, ok := value.(string); ok {
						if parsedTime, err := time.Parse(time.RFC3339, strValue); err == nil {
							alloc.Elem().Set(reflect.ValueOf(parsedTime))
							field.Set(alloc)
							nonEmpty = true
						}
					}
					continue
				}

				alloc.Elem().Set(reflect.ValueOf(value).Convert(elemType))
				field.Set(alloc)
				nonEmpty = true
			}
		}
	}

	return nonEmpty
}

func getFieldName(structField reflect.StructField) string {
	tag := structField.Tag.Get("json")
	if tag == "" {
		return structField.Name
	}

	return strings.Split(tag, ",")[0]
}

func matchToResource(match *bleveSearch.DocumentMatch) *search.Resource {
	// Extract Metadata.* fields from the flat bleve field map
	metadata := make(map[string]string)
	for k, v := range match.Fields {
		if strings.HasPrefix(k, "Metadata.") {
			if s, ok := v.(string); ok {
				metadata[strings.TrimPrefix(k, "Metadata.")] = s
			}
		}
	}
	if len(metadata) == 0 {
		metadata = nil
	}

	return &search.Resource{
		ID:       getFieldValue[string](match.Fields, "ID"),
		RootID:   getFieldValue[string](match.Fields, "RootID"),
		Path:     getFieldValue[string](match.Fields, "Path"),
		ParentID: getFieldValue[string](match.Fields, "ParentID"),
		Type:     uint64(getFieldValue[float64](match.Fields, "Type")),
		Deleted:  getFieldValue[bool](match.Fields, "Deleted"),
		Document: content.Document{
			Name:      getFieldValue[string](match.Fields, "Name"),
			Title:     getFieldValue[string](match.Fields, "Title"),
			Size:      uint64(getFieldValue[float64](match.Fields, "Size")),
			Mtime:     getFieldValue[string](match.Fields, "Mtime"),
			MimeType:  getFieldValue[string](match.Fields, "MimeType"),
			Content:   getFieldValue[string](match.Fields, "Content"),
			Tags:      getFieldSliceValue[string](match.Fields, "Tags"),
			Favorites: getFieldSliceValue[string](match.Fields, "Favorites"),
			Metadata:  metadata,
			Audio:     getAudioValue[libregraph.Audio](match.Fields),
			Image:     getImageValue[libregraph.Image](match.Fields),
			Location:  getLocationValue[libregraph.GeoCoordinates](match.Fields),
			Photo:     getPhotoValue[libregraph.Photo](match.Fields),
		},
	}
}

func escapeQuery(s string) string {
	return queryEscape.ReplaceAllString(s, "\\$1")
}
