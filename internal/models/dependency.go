package models

import "time"

type PackageDependency struct {
	ID           string      `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	RepositoryID string      `gorm:"type:uuid;index" json:"repository_id"`
	Repository   *Repository `gorm:"foreignKey:RepositoryID" json:"repository,omitempty"`

	Name           string `gorm:"type:varchar(500);index" json:"name"`
	CurrentVersion string `gorm:"type:varchar(255)" json:"current_version"`
	LatestVersion  string `gorm:"type:varchar(255)" json:"latest_version"`
	Ecosystem      string `gorm:"type:varchar(50)" json:"ecosystem"`
	ManifestFile   string `gorm:"type:varchar(500)" json:"manifest_file"`

	IsDirectDependency bool        `gorm:"default:true" json:"is_direct_dependency"`
	IsVulnerable       bool        `gorm:"default:false;index" json:"is_vulnerable"`
	VulnerabilityCVEs  StringArray `gorm:"column:vulnerability_cves;type:text[]" json:"vulnerability_cves"`
	UpdateAvailable    bool        `gorm:"default:false" json:"update_available"`

	LastScannedAt time.Time `json:"last_scanned_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (PackageDependency) TableName() string {
	return "package_dependencies"
}
