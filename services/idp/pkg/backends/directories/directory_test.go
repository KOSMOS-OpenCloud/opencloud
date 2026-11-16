package directories

import "testing"

func TestParseDirectories(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s, err := ParseDirectories("")
		if err != nil || len(s.Names()) != 0 {
			t.Fatalf("empty must yield empty set, got %v err=%v", s.Names(), err)
		}
	})
	t.Run("valid nu", func(t *testing.T) {
		s, err := ParseDirectories(`[{"name":"nu","label":"NU","uris":["ldap://10.1.24.228"],"dn_template":"uid=%s,ou=Users,dc=nugmbh,dc=p,dc=nu"}]`)
		if err != nil {
			t.Fatal(err)
		}
		dir, ok := s.Pick("NU") // case-insensitive
		if !ok {
			t.Fatal("nu not found")
		}
		if dir.BindDN("mouse") != "uid=mouse,ou=Users,dc=nugmbh,dc=p,dc=nu" {
			t.Fatalf("unexpected bind dn: %s", dir.BindDN("mouse"))
		}
	})
	t.Run("internal entry ignored", func(t *testing.T) {
		s, err := ParseDirectories(`[{"name":"internal","uris":["x"],"dn_template":"uid=%s"}]`)
		if err != nil || len(s.Names()) != 0 {
			t.Fatalf("internal must be skipped, got %v err=%v", s.Names(), err)
		}
	})
	for _, tc := range []struct{ name, blob string }{
		{"missing dn_template", `[{"name":"nu","uris":["x"]}]`},
		{"dn_template without %s", `[{"name":"nu","uris":["x"],"dn_template":"uid=x,ou=Users"}]`},
		{"missing uris", `[{"name":"nu","dn_template":"uid=%s"}]`},
		{"no name", `[{"uris":["x"],"dn_template":"uid=%s"}]`},
		{"bad json", `not json`},
		{"duplicate", `[{"name":"nu","uris":["x"],"dn_template":"uid=%s"},{"name":"NU","uris":["y"],"dn_template":"uid=%s"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseDirectories(tc.blob); err == nil {
				t.Fatalf("%s: expected error", tc.name)
			}
		})
	}
}

func TestIsInternal(t *testing.T) {
	for _, in := range []string{"", "internal", "INTERNAL", "  internal  "} {
		if !IsInternal(in) {
			t.Fatalf("%q should be internal", in)
		}
	}
	for _, ex := range []string{"nu", "extern", "other"} {
		if IsInternal(ex) {
			t.Fatalf("%q should be external", ex)
		}
	}
}

func TestBindDNEscapes(t *testing.T) {
	d := Directory{DNTemplate: "uid=%s,ou=Users,dc=nugmbh,dc=p,dc=nu"}
	// a username carrying a comma must not break out of the RDN
	got := d.BindDN("a,dc=evil")
	want := `uid=a\,dc=evil,ou=Users,dc=nugmbh,dc=p,dc=nu`
	if got != want {
		t.Fatalf("escaping failed:\n got %q\nwant %q", got, want)
	}
}
