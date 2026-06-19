package model

import "time"

type URL struct {
	ID          int64      `json:"id"`
	ShortCode   string     `json:"short_code"`
	OriginalURL string     `json:"original_url"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	ClickCount  int64      `json:"click_count"`
}

func (u *URL) IsExpired() bool {
	return u.ExpiresAt != nil && time.Now().After(*u.ExpiresAt)
}
