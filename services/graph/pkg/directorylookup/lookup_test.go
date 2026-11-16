package directorylookup

import (
	"testing"

	"github.com/opencloud-eu/opencloud/pkg/shared"
)

func dirWith(caCertFile string) shared.Directory {
	return shared.Directory{Name: "test", CACertFile: caCertFile}
}

func TestBuildBindDN(t *testing.T) {
	cases := []struct {
		name     string
		template string
		login    string
		want     string
	}{
		{
			name:     "plain",
			template: "uid=%s,ou=Users,dc=nugmbh,dc=p,dc=nu",
			login:    "mouse",
			want:     "uid=mouse,ou=Users,dc=nugmbh,dc=p,dc=nu",
		},
		{
			name:     "escapes DN special chars (no injection)",
			template: "uid=%s,ou=Users,dc=nu",
			login:    "a,b+c",
			want:     "uid=a\\,b\\+c,ou=Users,dc=nu",
		},
		{
			name:     "escapes leading space",
			template: "uid=%s,dc=nu",
			login:    " x",
			want:     "uid=\\ x,dc=nu",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildBindDN(tc.template, tc.login); got != tc.want {
				t.Fatalf("buildBindDN(%q, %q) = %q, want %q", tc.template, tc.login, got, tc.want)
			}
		})
	}
}

func TestDirectoryTLSNoCA(t *testing.T) {
	cfg, err := directoryTLS(dirWith(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil tls config when no CA file is set, got %v", cfg)
	}
}

func TestDirectoryTLSMissingCA(t *testing.T) {
	if _, err := directoryTLS(dirWith("/does/not/exist.pem")); err == nil {
		t.Fatal("expected an error for a missing CA file, got nil")
	}
}
