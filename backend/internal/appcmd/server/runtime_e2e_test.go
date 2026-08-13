package server

import "testing"

func TestE2EOIDCStorageAllowed(t *testing.T) {
	tests := []struct {
		name              string
		databaseURL       string
		isolatedMemory    string
		postgresAvailable string
		want              bool
	}{
		{name: "isolated memory", isolatedMemory: "1", want: true},
		{name: "dedicated e2e PostgreSQL", databaseURL: "postgresql://tester@127.0.0.1:5432/asterrouter_e2e_oidc?sslmode=disable", postgresAvailable: "1", want: true},
		{name: "dedicated test PostgreSQL", databaseURL: "postgres://tester@127.0.0.1:5432/asterrouter-oidc-test?sslmode=disable", postgresAvailable: "1", want: true},
		{name: "marker missing", databaseURL: "postgresql://tester@127.0.0.1:5432/asterrouter_e2e_oidc", want: false},
		{name: "unsafe production name", databaseURL: "postgresql://tester@127.0.0.1:5432/asterrouter", postgresAvailable: "1", want: false},
		{name: "embedded token is not isolated", databaseURL: "postgresql://tester@127.0.0.1:5432/asterrouter_testdata", postgresAvailable: "1", want: false},
		{name: "non PostgreSQL URL", databaseURL: "https://127.0.0.1/asterrouter_e2e_oidc", postgresAvailable: "1", want: false},
		{name: "host missing", databaseURL: "postgresql:///asterrouter_e2e_oidc", postgresAvailable: "1", want: false},
		{name: "nested path", databaseURL: "postgresql://tester@127.0.0.1/cluster/asterrouter_e2e_oidc", postgresAvailable: "1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ASTER_DEV_ISOLATED_MEMORY", tt.isolatedMemory)
			t.Setenv("ASTER_E2E_POSTGRES_AVAILABLE", tt.postgresAvailable)
			if got := e2eOIDCStorageAllowed(tt.databaseURL); got != tt.want {
				t.Fatalf("e2eOIDCStorageAllowed(%q) = %v, want %v", tt.databaseURL, got, tt.want)
			}
		})
	}
}
