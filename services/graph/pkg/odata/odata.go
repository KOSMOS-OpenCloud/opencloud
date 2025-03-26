package odata

import (
	"strings"

	"github.com/CiscoM31/godata"
)

// GetExpandValues extracts the values of the $expand query parameter and
// returns them in a []string, rejecting any $expand value that consists of more
// than just a single path segment.
func GetExpandValues(req *godata.GoDataQuery) ([]string, error) {
	if req == nil || req.Expand == nil {
		return []string{}, nil
	}

	var expand []string
	for _, item := range req.Expand.ExpandItems {
		paths, err := collectExpandPaths(item, "")
		if err != nil {
			return nil, err
		}
		expand = append(expand, paths...)
	}

	return expand, nil
}

// collectExpandPaths recursively collects all valid expand paths from the given item.
func collectExpandPaths(item *godata.ExpandItem, prefix string) ([]string, error) {
	if err := validateExpandItem(item); err != nil {
		return nil, err
	}

	// Build the current path
	currentPath := prefix
	if len(item.Path) > 0 {
		if len(item.Path) > 1 {
			return nil, godata.NotImplementedError("multiple segments in $expand not supported")
		}
		if currentPath == "" {
			currentPath = item.Path[0].Value
		} else {
			currentPath += "." + item.Path[0].Value
		}
	}

	// Collect all paths, including nested ones
	paths := []string{currentPath}
	if item.Expand != nil {
		for _, subItem := range item.Expand.ExpandItems {
			subPaths, err := collectExpandPaths(subItem, currentPath)
			if err != nil {
				return nil, err
			}
			paths = append(paths, subPaths...)
		}
	}

	return paths, nil
}

// validateExpandItem checks if an expand item contains unsupported options.
func validateExpandItem(item *godata.ExpandItem) error {
	if item.Filter != nil || item.At != nil || item.Search != nil ||
		item.OrderBy != nil || item.Skip != nil || item.Top != nil ||
		item.Select != nil || item.Compute != nil || item.Levels != 0 {
		return godata.NotImplementedError("options for $expand not supported")
	}
	return nil
}

// GetSelectValues extracts the values of the $select query parameter and
// returns them in a []string, rejects any $select value that consists of more
// than just a single path segment
func GetSelectValues(req *godata.GoDataQuery) ([]string, error) {
	if req == nil || req.Select == nil {
		return []string{}, nil
	}
	sel := make([]string, 0, len(req.Select.SelectItems))
	for _, item := range req.Select.SelectItems {
		if len(item.Segments) > 1 {
			return []string{}, godata.NotImplementedError("multiple segments in $select not supported")
		}
		sel = append(sel, item.Segments[0].Value)
	}
	return sel, nil
}

// GetSearchValues extracts the value of the $search query parameter and returns
// it as a string. Rejects any search query that is more than just a simple string
func GetSearchValues(req *godata.GoDataQuery) (string, error) {
	if req == nil || req.Search == nil {
		return "", nil
	}

	// Only allow simple search queries for now
	if len(req.Search.Tree.Children) != 0 {
		return "", godata.NotImplementedError("complex search queries are not supported")
	}

	searchValue := strings.Trim(req.Search.Tree.Token.Value, "\"")
	return searchValue, nil
}
