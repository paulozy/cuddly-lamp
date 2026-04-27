package utils

import (
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

func TestParseRepositoryURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		wantName    string
		wantType    models.RepositoryType
		wantErr     bool
	}{
		{
			name:     "GitHub URL",
			url:      "https://github.com/owner/repo",
			wantName: "owner/repo",
			wantType: models.RepositoryTypeGitHub,
		},
		{
			name:     "GitHub URL with .git suffix",
			url:      "https://github.com/owner/repo.git",
			wantName: "owner/repo",
			wantType: models.RepositoryTypeGitHub,
		},
		{
			name:     "GitLab URL",
			url:      "https://gitlab.com/owner/repo",
			wantName: "owner/repo",
			wantType: models.RepositoryTypeGitLab,
		},
		{
			name:     "Gitea URL",
			url:      "https://gitea.example.com/owner/repo",
			wantName: "owner/repo",
			wantType: models.RepositoryTypeGitea,
		},
		{
			name:    "unsupported host",
			url:     "https://bitbucket.org/owner/repo",
			wantErr: true,
		},
		{
			name:    "missing repo segment",
			url:     "https://github.com/owner",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			url:     "not-a-url",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotType, err := ParseRepositoryURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (name=%q, type=%q)", gotName, gotType)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotName != tt.wantName {
				t.Errorf("name = %q, want %q", gotName, tt.wantName)
			}
			if gotType != tt.wantType {
				t.Errorf("type = %q, want %q", gotType, tt.wantType)
			}
		})
	}
}
