package app

import (
	"fmt"
	"github.com/go-sql-driver/mysql"
	"os"
	"strings"
)

type Config struct {
	DSN, Secret, Addr string
	Passwords         map[string]string
}

func LoadConfig() (Config, error) {
	c := Config{Secret: os.Getenv("SESSION_SECRET"), Addr: os.Getenv("API_ADDR"), Passwords: map[string]string{}}
	required := []string{"DB_HOST", "DB_NAME", "DB_USER", "DB_PASSWORD", "SESSION_SECRET", "SEED_ADMIN_PASSWORD", "SEED_REQUESTER_PASSWORD", "SEED_REQUESTER2_PASSWORD", "SEED_TECH_PASSWORD", "SEED_TECH2_PASSWORD", "SEED_OTHER_PASSWORD"}
	for _, k := range required {
		if strings.TrimSpace(os.Getenv(k)) == "" {
			return c, fmt.Errorf("missing required variable: %s", k)
		}
	}
	if len(c.Secret) < 32 {
		return c, fmt.Errorf("SESSION_SECRET must contain at least 32 characters")
	}
	for _, k := range required {
		if strings.HasPrefix(k, "SEED_") {
			v := os.Getenv(k)
			if len(v) < 12 || len(v) > 72 || strings.Contains(strings.ToLower(v), "change-me") {
				return c, fmt.Errorf("%s requires a non-default password of 12 to 72 bytes", k)
			}
			c.Passwords[k] = v
		}
	}
	mc := mysql.NewConfig()
	mc.User = os.Getenv("DB_USER")
	mc.Passwd = os.Getenv("DB_PASSWORD")
	mc.Net = "tcp"
	mc.Addr = os.Getenv("DB_HOST")
	mc.DBName = os.Getenv("DB_NAME")
	mc.ParseTime = true
	mc.Params = map[string]string{"charset": "utf8mb4"}
	mc.Collation = "utf8mb4_unicode_ci"
	c.DSN = mc.FormatDSN()
	if c.Addr == "" {
		c.Addr = ":8080"
	}
	return c, nil
}
