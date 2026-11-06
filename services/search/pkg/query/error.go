package query

import (
	"fmt"

	"github.com/opencloud-eu/opencloud/pkg/ast"
)

// StartsWithBinaryOperatorError records an error and the operation that caused it.
type StartsWithBinaryOperatorError struct {
	Node *ast.OperatorNode
}

func (e StartsWithBinaryOperatorError) Error() string {
	return "the expression can't begin from a binary operator: '" + e.Node.Value + "'"
}

// NamedGroupInvalidNodesError records an error and the operation that caused it.
type NamedGroupInvalidNodesError struct {
	Node ast.Node
}

func (e NamedGroupInvalidNodesError) Error() string {
	return fmt.Errorf(
		"'%T' - '%v' - '%v' is not valid",
		e.Node,
		ast.NodeKey(e.Node),
		ast.NodeValue(e.Node),
	).Error()
}

// UnsupportedTimeRangeError records an error and the value that caused it.
type UnsupportedTimeRangeError struct {
	Value any
}

func (e UnsupportedTimeRangeError) Error() string {
	return fmt.Sprintf("unable to convert '%v' to a time range", e.Value)
}

// WildcardOnlyError records a value that consists of wildcards only. Such a
// value expands to every term of the index (or of all fields) and can exhaust
// the memory of the search service.
type WildcardOnlyError struct {
	Key   string
	Value string
}

func (e WildcardOnlyError) Error() string {
	if e.Key == "" {
		return fmt.Sprintf("wildcard-only search '%s' is not allowed", e.Value)
	}
	return fmt.Sprintf("wildcard-only value '%s:%s' is not allowed", e.Key, e.Value)
}

func IsValidationError(err error) bool {
	switch err.(type) {
	case *StartsWithBinaryOperatorError, *NamedGroupInvalidNodesError, *UnsupportedTimeRangeError, *WildcardOnlyError:
		return true
	}
	return false
}
