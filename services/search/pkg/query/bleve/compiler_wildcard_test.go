package bleve

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencloud-eu/opencloud/services/search/pkg/query"
)

// A value without any literal character expands to every term of the
// index and exhausted the memory of the search service (cloud.brandis.eu,
// 2026-11-06: "Metadata.doc.meta_source:*" parsed into freetext "*").
func TestCompileRejectsWildcardOnly(t *testing.T) {
	for _, kqlText := range []string{
		`*`,
		`**`,
		`"*"`,
		`?*`,
		`name:*`,
		`name:"*"`,
		`metadata.doc.meta_source:*`,
		`metadata.doc.type:rechnung AND *`,
	} {
		_, err := DefaultCreator.Create(kqlText)
		if err == nil {
			t.Errorf("%s: compiled, want WildcardOnlyError", kqlText)
			continue
		}
		if !query.IsValidationError(err) {
			t.Errorf("%s: error %T (%v) is no validation error", kqlText, err, err)
		}
	}
}

func TestCompileAllowsWildcardsWithLiteral(t *testing.T) {
	for _, kqlText := range []string{
		`name:*.pdf`,
		`name:"*rechnung*"`,
		`rechnung*`,
	} {
		if _, err := DefaultCreator.Create(kqlText); err != nil {
			t.Errorf("%s: %v", kqlText, err)
		}
	}
}

// Metadata keys with underscores and digits compile to a field query
// instead of falling apart into freetext terms.
func TestCompileMetadataKeys(t *testing.T) {
	cases := map[string]string{
		`metadata.booking.doc_type:IfrEingangsrechnung`:                `metadata.booking.doc_type:IfrEingangsrechnung`,
		`metadata.sender.vat_id:DE123456789`:                           `metadata.sender.vat_id:DE123456789`,
		`metadata.doc.type:rechnung AND NOT metadata.booking.status:E`: `"must_not":{"disjuncts":[{"query":"metadata.booking.status:E"}]`,
		// The group key must survive a preceding NOT.
		`metadata.doc.type:rechnung AND NOT metadata.booking.status:(B OR E)`: `{"query":"metadata.booking.status:B"},{"query":"metadata.booking.status:E"}`,
	}
	for kqlText, wantFragment := range cases {
		compiled, err := DefaultCreator.Create(kqlText)
		if err != nil {
			t.Errorf("%s: %v", kqlText, err)
			continue
		}
		compiledJSON, _ := json.Marshal(compiled)
		if !strings.Contains(string(compiledJSON), wantFragment) {
			t.Errorf("%s: compiled to %s, want fragment %s", kqlText, compiledJSON, wantFragment)
		}
	}
}
