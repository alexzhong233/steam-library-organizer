package main

import (
	"path/filepath"
	"testing"
)

func TestLoginUsersFormats(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
		fail             bool
	}{
		{"mac-autologin", `"users" { "76561197960278073" { "AutoLogin" "1" "timestamp" "1793265947" } }`, "12345", false},
		{"recent", `"Users" { "76561197960265729" { "MostRecent" "1" } "76561197960265730" { "AutoLogin" "1" } }`, "1", false},
		{"auto-among-many", `"users" { "76561197960265729" { "AutoLogin" "1" } "76561197960265730" { "timestamp" "9999999999" } }`, "1", false},
		{"sole-user", `"users" { "76561197960265729" { "PersonaName" "Name" } }`, "1", false},
		{"ambiguous", `"users" { "76561197960265729" {} "76561197960265730" {} }`, "", true},
		{"two-auto", `"users" { "76561197960265729" {"AutoLogin" "1"} "76561197960265730" {"AutoLogin" "1"} }`, "", true},
		{"invalid-id", `"users" { "123" {"AutoLogin" "1"} }`, "", true},
		{"unclosed", `"users" { "76561197960265729" {"AutoLogin" "1"}`, "", true},
		{"duplicate", `"users" { "76561197960265729" {"AutoLogin" "1" "autologin" "0"} }`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := accountFromLoginUsers(tc.data)
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestToolDirectory(t *testing.T) {
	for _, tc := range []struct{ exe, want string }{
		{filepath.Join("portable", "SteamCollectionImporter.exe"), "portable"},
		{filepath.Join("portable", "SteamCollectionImporter.app", "Contents", "MacOS", "SteamCollectionImporter"), "portable"},
		{filepath.Join("portable", "SteamCollectionImporter"), "portable"},
	} {
		if got := toolDirectory(tc.exe); got != tc.want {
			t.Fatalf("%s: %s", tc.exe, got)
		}
	}
	m, _, err := loadConfiguredManifest("")
	if err != nil || len(m.Games) != 0 {
		t.Fatal("startup must not load a personal list", m.GameCount, err)
	}
}
