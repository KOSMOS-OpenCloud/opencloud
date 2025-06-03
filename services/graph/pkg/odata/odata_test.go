package odata

import (
	"testing"

	"github.com/CiscoM31/godata"
	"github.com/stretchr/testify/assert"
)

func TestGetExpandValues(t *testing.T) {
	t.Run("NilRequest", func(t *testing.T) {
		result, err := GetExpandValues(nil)
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("EmptyExpand", func(t *testing.T) {
		req := &godata.GoDataQuery{}
		result, err := GetExpandValues(req)
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("SinglePathSegment", func(t *testing.T) {
		req := &godata.GoDataQuery{
			Expand: &godata.GoDataExpandQuery{
				ExpandItems: []*godata.ExpandItem{
					{Path: []*godata.Token{{Value: "orders"}}},
				},
			},
		}
		result, err := GetExpandValues(req)
		assert.NoError(t, err)
		assert.Equal(t, []string{"orders"}, result)
	})

	t.Run("MultiplePathSegments", func(t *testing.T) {
		req := &godata.GoDataQuery{
			Expand: &godata.GoDataExpandQuery{
				ExpandItems: []*godata.ExpandItem{
					{Path: []*godata.Token{{Value: "orders"}, {Value: "details"}}},
				},
			},
		}
		result, err := GetExpandValues(req)
		assert.Error(t, err)
		assert.Empty(t, result)
	})

	t.Run("NestedExpand", func(t *testing.T) {
		req := &godata.GoDataQuery{
			Expand: &godata.GoDataExpandQuery{
				ExpandItems: []*godata.ExpandItem{
					{
						Path: []*godata.Token{{Value: "items"}},
						Expand: &godata.GoDataExpandQuery{
							ExpandItems: []*godata.ExpandItem{
								{
									Path: []*godata.Token{{Value: "subitem"}},
									Expand: &godata.GoDataExpandQuery{
										ExpandItems: []*godata.ExpandItem{
											{Path: []*godata.Token{{Value: "subsubitems"}}},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		result, err := GetExpandValues(req)
		assert.NoError(t, err)
		assert.Subset(t, result, []string{"items", "items.subitem", "items.subitem.subsubitems"}, "must contain all levels of expansion")
	})
}

func TestGetSelectValues(t *testing.T) {
	t.Run("NilRequest", func(t *testing.T) {
		result, err := GetSelectValues(nil)
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("EmptySelect", func(t *testing.T) {
		req := &godata.GoDataQuery{}
		result, err := GetSelectValues(req)
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("SinglePathSegment", func(t *testing.T) {
		req := &godata.GoDataQuery{
			Select: &godata.GoDataSelectQuery{
				SelectItems: []*godata.SelectItem{
					{Segments: []*godata.Token{{Value: "name"}}},
				},
			},
		}
		result, err := GetSelectValues(req)
		assert.NoError(t, err)
		assert.Equal(t, []string{"name"}, result)
	})

	t.Run("MultiplePathSegments", func(t *testing.T) {
		req := &godata.GoDataQuery{
			Select: &godata.GoDataSelectQuery{
				SelectItems: []*godata.SelectItem{
					{Segments: []*godata.Token{{Value: "name"}, {Value: "first"}}},
				},
			},
		}
		result, err := GetSelectValues(req)
		assert.Error(t, err)
		assert.Empty(t, result)
	})
}

func TestGetSearchValues(t *testing.T) {
	t.Run("NilRequest", func(t *testing.T) {
		result, err := GetSearchValues(nil)
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("EmptySearch", func(t *testing.T) {
		req := &godata.GoDataQuery{}
		result, err := GetSearchValues(req)
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("SimpleSearch", func(t *testing.T) {
		req := &godata.GoDataQuery{
			Search: &godata.GoDataSearchQuery{
				Tree: &godata.ParseNode{
					Token: &godata.Token{Value: "test"},
				},
			},
		}
		result, err := GetSearchValues(req)
		assert.NoError(t, err)
		assert.Equal(t, "test", result)
	})

	t.Run("ComplexSearch", func(t *testing.T) {
		req := &godata.GoDataQuery{
			Search: &godata.GoDataSearchQuery{
				Tree: &godata.ParseNode{
					Children: []*godata.ParseNode{
						{Token: &godata.Token{Value: "test"}},
					},
				},
			},
		}
		result, err := GetSearchValues(req)
		assert.Error(t, err)
		assert.Empty(t, result)
	})
}
