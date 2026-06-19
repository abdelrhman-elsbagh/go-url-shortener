package handler

type EncodeRequestDoc struct {
	URL string `json:"url" example:"https://www.bbc.com/news/world-europe-12345678"`
}

type EncodeResponseDoc struct {
	ShortURL    string `json:"short_url" example:"http://localhost:8080/Xk92mP"`
	ShortCode   string `json:"short_code" example:"Xk92mP"`
	OriginalURL string `json:"original_url" example:"https://www.bbc.com/news/world-europe-12345678"`
	CreatedAt   string `json:"created_at" example:"2026-06-19T12:30:00Z"`
}

type DecodeRequestDoc struct {
	ShortCode string `json:"short_code" example:"Xk92mP"`
}

type DecodeResponseDoc struct {
	OriginalURL string `json:"original_url" example:"https://www.bbc.com/news/world-europe-12345678"`
	ShortCode   string `json:"short_code" example:"Xk92mP"`
	CreatedAt   string `json:"created_at" example:"2026-06-19T12:30:00Z"`
	ClickCount  int64  `json:"click_count" example:"42"`
}

type HealthResponseDoc struct {
	Status  string `json:"status" example:"ok"`
	Version string `json:"version" example:"1.0.0"`
}

type MetricsResponseDoc struct {
	EncodeTotal int64 `json:"encode_total" example:"18"`
	DecodeTotal int64 `json:"decode_total" example:"15"`
	CacheHit    int64 `json:"cache_hit" example:"7"`
	CacheMiss   int64 `json:"cache_miss" example:"8"`
	ErrorTotal  int64 `json:"error_total" example:"1"`
}

type APIErrorDoc struct {
	Code    string `json:"code" example:"INVALID_URL"`
	Message string `json:"message" example:"invalid URL"`
	Details string `json:"details,omitempty" example:"scheme not allowed"`
}

type ErrorResponseDoc struct {
	Error APIErrorDoc `json:"error"`
}
