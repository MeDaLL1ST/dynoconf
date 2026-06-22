package httpserver

import (
	"net/http"
	"testing"

	"github.com/dynoconf/dynoconf/internal/store"
)

func TestDecideServiceAccess(t *testing.T) {
	cases := []struct {
		name        string
		role        string
		level       string
		needEditor  bool
		wantAllowed bool
		wantStatus  int
	}{
		{"admin reads anything", store.RoleAdmin, "", false, true, http.StatusOK},
		{"admin writes anything", store.RoleAdmin, "", true, true, http.StatusOK},
		{"editor can read", store.RoleUser, store.LevelEditor, false, true, http.StatusOK},
		{"editor can write", store.RoleUser, store.LevelEditor, true, true, http.StatusOK},
		{"viewer can read", store.RoleUser, store.LevelViewer, false, true, http.StatusOK},
		{"viewer cannot write", store.RoleUser, store.LevelViewer, true, false, http.StatusForbidden},
		{"no perm read -> not found", store.RoleUser, "", false, false, http.StatusNotFound},
		{"no perm write -> not found", store.RoleUser, "", true, false, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed, status := decideServiceAccess(tc.role, tc.level, tc.needEditor)
			if allowed != tc.wantAllowed {
				t.Errorf("allowed = %v, want %v", allowed, tc.wantAllowed)
			}
			if !allowed && status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
		})
	}
}
