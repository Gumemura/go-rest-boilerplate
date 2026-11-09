package domain

import (
	"errors"
	"strings"
	"time"
)

const (
	RoleAdmin = 1
	RolePlayer = 2
	RoleAnonymousPlayer = 3
)

var (
	ErrEmptyInstallationId = errors.New("installation id cannot be empty")
)

type Player struct {
	Id                string
	InstallationId    string
	Email             string
	Password          string
	RoleId            int
	CreatedAt         time.Time
	UpdatedAt         *time.Time
	DeletedAt         *time.Time
	PasswordChangedAt *time.Time
}

func (p Player) IsAdmin() bool { return p.RoleId == RoleAdmin }

func NewAnonymousPlayer(id, installationID string) (*Player, error) {
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return nil, ErrEmptyInstallationId
	}
	return &Player{
		Id:             id,
		InstallationId: installationID,
		CreatedAt:      time.Now().UTC(),
	}, nil
}

func (p Player) TokensRevokedBefore() time.Time {
	if p.PasswordChangedAt == nil {
		return time.Time{}
	}
	return *p.PasswordChangedAt
}