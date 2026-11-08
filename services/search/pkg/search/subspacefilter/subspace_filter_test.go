package subspacefilter_test

import (
	"encoding/json"
	"testing"

	rpcv1beta1 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
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

func TestSubspaceAccessHitVisible(t *testing.T) {
	// space root member who is not a member of ./stats (regression: such hits
	// leaked names and metadata of subspace content)
	rootMember := &search.SubspaceAccess{
		Allowed: []string{"./hr"},
		Denied:  []string{"./stats"},
	}
	// subspace-only user, member of ./hr only
	subspaceOnly := &search.SubspaceAccess{
		RestrictToAllowed: true,
		Allowed:           []string{"./hr"},
		Denied:            []string{"./stats"},
	}
	// subspace-only user without any accessible subspace
	subspaceOnlyNone := &search.SubspaceAccess{
		RestrictToAllowed: true,
		Denied:            []string{"./stats", "./hr"},
	}
	// nested: member of ./hr, but not of ./hr/salaries
	nested := &search.SubspaceAccess{
		Allowed: []string{"./hr"},
		Denied:  []string{"./hr/salaries"},
	}

	tests := []struct {
		name     string
		access   *search.SubspaceAccess
		hitPath  string
		expected bool
	}{
		{"nil access shows everything", nil, "./stats/report.pdf", true},
		{"root member: hit outside subspaces", rootMember, "./legal/contract.pdf", true},
		{"root member: hit in own subspace", rootMember, "./hr/list.xlsx", true},
		{"root member: hit in foreign subspace", rootMember, "./stats/report.pdf", false},
		{"root member: foreign subspace root", rootMember, "./stats", false},
		{"root member: prefix collision", rootMember, "./statsarchive/a.pdf", true},
		{"subspace-only: hit in own subspace", subspaceOnly, "./hr/list.xlsx", true},
		{"subspace-only: hit outside subspaces", subspaceOnly, "./legal/contract.pdf", false},
		{"subspace-only: hit in foreign subspace", subspaceOnly, "./stats/report.pdf", false},
		{"subspace-only without subspaces sees nothing", subspaceOnlyNone, "./legal/contract.pdf", false},
		{"nested: denied inner subspace wins", nested, "./hr/salaries/2026.xlsx", false},
		{"nested: allowed outer subspace", nested, "./hr/list.xlsx", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := tt.access.HitVisible(tt.hitPath); result != tt.expected {
				t.Errorf("HitVisible(%q) = %v, want %v", tt.hitPath, result, tt.expected)
			}
		})
	}
}

func TestSubspaceAccessPermissionsFor(t *testing.T) {
	outerPerms := &sprovider.ResourcePermissions{Stat: true, InitiateFileDownload: true}
	innerPerms := &sprovider.ResourcePermissions{Stat: true, InitiateFileDownload: true, InitiateFileUpload: true}
	access := &search.SubspaceAccess{
		Allowed: []string{"./hr", "./hr/team"},
		AllowedPermissions: map[string]*sprovider.ResourcePermissions{
			"./hr":      outerPerms,
			"./hr/team": innerPerms,
		},
	}

	if got := access.PermissionsFor("./hr/list.xlsx"); got != outerPerms {
		t.Errorf("expected outer subspace permissions, got %v", got)
	}
	if got := access.PermissionsFor("./hr/team/plan.docx"); got != innerPerms {
		t.Errorf("expected innermost subspace permissions, got %v", got)
	}
	if got := access.PermissionsFor("./legal/contract.pdf"); got != nil {
		t.Errorf("expected nil outside subspaces, got %v", got)
	}
	var nilAccess *search.SubspaceAccess
	if got := nilAccess.PermissionsFor("./hr/list.xlsx"); got != nil {
		t.Errorf("expected nil for nil access, got %v", got)
	}
}

func TestIsSubspaceMember(t *testing.T) {
	okStatus := &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_OK}
	bypassOpaque := &typesv1beta1.Opaque{Map: map[string]*typesv1beta1.OpaqueEntry{
		"subspace-management-bypass": {Decoder: "plain", Value: []byte("true")},
	}}
	allPerms := &sprovider.ResourcePermissions{Stat: true, InitiateFileDownload: true, AddGrant: true}

	tests := []struct {
		name     string
		statRes  *sprovider.StatResponse
		expected bool
	}{
		{"member with download", &sprovider.StatResponse{Status: okStatus, Info: &sprovider.ResourceInfo{PermissionSet: allPerms}}, true},
		{"bypass only (global space admin, no grant)", &sprovider.StatResponse{Status: okStatus, Info: &sprovider.ResourceInfo{PermissionSet: allPerms, Opaque: bypassOpaque}}, false},
		{"stat without download", &sprovider.StatResponse{Status: okStatus, Info: &sprovider.ResourceInfo{PermissionSet: &sprovider.ResourcePermissions{Stat: true, ListContainer: true}}}, false},
		{"not found", &sprovider.StatResponse{Status: &rpcv1beta1.Status{Code: rpcv1beta1.Code_CODE_NOT_FOUND}}, false},
		{"nil response", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := search.IsSubspaceMember(tt.statRes); result != tt.expected {
				t.Errorf("IsSubspaceMember() = %v, want %v", result, tt.expected)
			}
		})
	}
}
