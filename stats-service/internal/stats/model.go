package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type DayCount struct {
	Date   string `json:"date"`
	Clicks int64  `json:"clicks"`
}

type Summary struct {
	LinkID       string     `json:"linkId"`
	TotalClicks  int64      `json:"totalClicks"`
	ClicksPerDay []DayCount `json:"clicksPerDay"`
}

type Total struct {
	LinkID      string `json:"linkId"`
	TotalClicks int64  `json:"totalClicks"`
}

type Click struct {
	EventID   uuid.UUID
	LinkID    uuid.UUID
	Slug      string
	ClickedAt time.Time
}

type Repository interface {
	Totals(context.Context, []uuid.UUID) (map[uuid.UUID]int64, error)
	Summary(context.Context, uuid.UUID) (Summary, error)
	InsertBatch(context.Context, []Click) error
}

// Um token de versão impede que uma consulta concorrente republique cache antigo
// depois de o consumidor invalidar o resumo.
type SummaryCache interface {
	Get(context.Context, uuid.UUID) ([]byte, string, error)
	Set(context.Context, uuid.UUID, string, []byte) error
	Invalidate(context.Context, []uuid.UUID) error
}

func parseClick(data []byte) (Click, error) {
	var event struct {
		EventID   string `json:"eventId"`
		LinkID    string `json:"linkId"`
		Slug      string `json:"slug"`
		ClickedAt string `json:"clickedAt"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return Click{}, err
	}
	eventID, err := uuid.Parse(event.EventID)
	if err != nil {
		return Click{}, fmt.Errorf("eventId inválido: %w", err)
	}
	linkID, err := uuid.Parse(event.LinkID)
	if err != nil {
		return Click{}, fmt.Errorf("linkId inválido: %w", err)
	}
	if len(event.Slug) != 7 {
		return Click{}, fmt.Errorf("slug inválido")
	}
	for _, c := range []byte(event.Slug) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return Click{}, fmt.Errorf("slug inválido")
		}
	}
	clickedAt, err := time.Parse(time.RFC3339Nano, event.ClickedAt)
	if err != nil {
		return Click{}, fmt.Errorf("clickedAt inválido: %w", err)
	}
	return Click{eventID, linkID, event.Slug, clickedAt.UTC()}, nil
}
