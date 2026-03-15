package domain

import "time"

type UsageRecord struct {
	ID        int64     `json:"id"`
	Service   string    `json:"service"`
	Model     string    `json:"model"`
	TokensIn  int64     `json:"tokens_in"`
	TokensOut int64     `json:"tokens_out"`
	Cost      float64   `json:"cost"`
	CreatedAt time.Time `json:"created_at"`
}

type UsageFilter struct {
	Service string
	From    *time.Time
	To      *time.Time
}

type UsageTotal struct {
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	Cost      float64 `json:"cost"`
}

type Subscription struct {
	Service    string     `json:"service"`
	Plan       string     `json:"plan"`
	LimitValue *int64     `json:"limit_value"`
	Used       int64      `json:"used"`
	ResetDate  *time.Time `json:"reset_date"`
}

type LimitItem struct {
	Service    string      `json:"service"`
	Plan       string      `json:"plan"`
	LimitValue *int64      `json:"limit_value"`
	Used       int64       `json:"used"`
	ResetDate  *time.Time  `json:"reset_date"`
	Percentage interface{} `json:"percentage"`
}

type ProviderStatus struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
}
