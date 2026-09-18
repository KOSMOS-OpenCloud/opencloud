package subspacefilter_test

import (
	"encoding/json"
	"testing"

	sprovider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	typesv1beta1 "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/opencloud-eu/opencloud/services/search/pkg/search"
)

func TestIsPathInAllowedSubspaces(t *testing.T) {
	allowed := []string{"./finance/budget", "./hr"}

	tests := []struct {
		name     string
		hitPath  string
		allowed  []string
		expected bool
	}{
		// Positive: inside subspace
		{"exact subspace path", "./finance/budget", allowed, true},
		{"file inside subspace", "./finance/budget/report.pdf", allowed, true},
		{"nested dir inside subspace", "./finance/budget/2026/q1", allowed, true},
		{"second subspace root", "./hr", allowed, true},
		{"file in second subspace", "./hr/employees.xlsx", allowed, true},

		// Negative: outside subspaces
		{"sibling dir", "./finance/taxes", allowed, false},
		{"parent dir", "./finance", allowed, false},
		{"unrelated dir", "./legal", allowed, false},
		{"root", ".", allowed, false},
		{"file at root", "./readme.txt", allowed, false},

		// Edge cases
		{"empty allowed list", "./finance/budget", nil, false},
		{"empty allowed list 2", "./anything", []string{}, false},
		{"prefix collision", "./hr-archive/doc.pdf", allowed, false},
		{"subspace name as prefix of another dir", "./finance/budgets", allowed, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := search.IsPathInAllowedSubspaces(tt.hitPath, tt.allowed)
			if result != tt.expected {
				t.Errorf("IsPathInAllowedSubspaces(%q, %v) = %v, want %v", tt.hitPath, tt.allowed, result, tt.expected)
			}
		})
	}
}

func TestSubspaceEntriesFromOpaque(t *testing.T) {
	t.Run("nil opaque returns nil", func(t *testing.T) {
		entries := search.SubspaceEntriesFromOpaque(nil)
		if entries != nil {
			t.Errorf("expected nil, got %v", entries)
		}
	})

	t.Run("no subspaces key returns nil", func(t *testing.T) {
		opaque := &typesv1beta1.Opaque{
			Map: map[string]*typesv1beta1.OpaqueEntry{
				"other": {Value: []byte("data")},
			},
		}
		entries := search.SubspaceEntriesFromOpaque(opaque)
		if entries != nil {
			t.Errorf("expected nil, got %v", entries)
		}
	})

	t.Run("valid subspaces", func(t *testing.T) {
		type entry struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		}
		data, _ := json.Marshal([]entry{
			{ID: "node1", Path: "/finance/budget"},
			{ID: "node2", Path: "/hr"},
		})
		opaque := &typesv1beta1.Opaque{
			Map: map[string]*typesv1beta1.OpaqueEntry{
				"subspaces": {Value: data},
			},
		}
		entries := search.SubspaceEntriesFromOpaque(opaque)
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}
	})

	t.Run("invalid JSON returns nil", func(t *testing.T) {
		opaque := &typesv1beta1.Opaque{
			Map: map[string]*typesv1beta1.OpaqueEntry{
				"subspaces": {Value: []byte("not json")},
			},
		}
		entries := search.SubspaceEntriesFromOpaque(opaque)
		if entries != nil {
			t.Errorf("expected nil, got %v", entries)
		}
	})
}

func TestIsSubspaceOnlyUser(t *testing.T) {
	tests := []struct {
		name     string
		perms    *sprovider.ResourcePermissions
		expected bool
	}{
		{
			"full member (has download)",
			&sprovider.ResourcePermissions{Stat: true, InitiateFileDownload: true, ListContainer: true},
			false,
		},
		{
			"subspace-only user (stat+list only)",
			&sprovider.ResourcePermissions{Stat: true, ListContainer: true, GetPath: true},
			true,
		},
		{
			"nil permissions",
			nil,
			false,
		},
		{
			"no stat (no access at all)",
			&sprovider.ResourcePermissions{},
			false,
		},
		{
			"editor (has upload)",
			&sprovider.ResourcePermissions{Stat: true, InitiateFileUpload: true},
			false,
		},
		{
			"manager (has addGrant)",
			&sprovider.ResourcePermissions{Stat: true, AddGrant: true},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := search.IsSubspaceOnlyUser(tt.perms)
			if result != tt.expected {
				t.Errorf("IsSubspaceOnlyUser(%v) = %v, want %v", tt.perms, result, tt.expected)
			}
		})
	}
}
