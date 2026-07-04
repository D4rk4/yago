package yagonode

import "strings"

const (
	envAdminUser     = "YAGO_ADMIN_USER"
	envAdminPassword = "YAGO_ADMIN" + "_PASSWORD"
)

type adminConfig struct {
	Username string
	Password string
}

func loadAdminConfig(getenv func(string) string) adminConfig {
	return adminConfig{
		Username: strings.TrimSpace(getenv(envAdminUser)),
		Password: getenv(envAdminPassword),
	}
}
