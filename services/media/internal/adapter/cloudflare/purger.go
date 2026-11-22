package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"api/media/internal/domain/event"
	"api/media/internal/domain/model"
	"api/media/internal/domain/port"

	"github.com/rs/zerolog/log"
)

var (
	_ port.CachePurger   = (*CachePurger)(nil)
	_ event.EventHandler = (*AssetDeletedPurgeSubscriber)(nil)
)

type CachePurger struct {
	zoneID   string
	apiToken string
	client   *http.Client
	baseURL  string
}

func NewCachePurger(zoneID, apiToken string) *CachePurger {
	return &CachePurger{
		zoneID:   zoneID,
		apiToken: apiToken,
		client:   &http.Client{Timeout: 10 * time.Second},
		baseURL:  "https://api.cloudflare.com/client/v4",
	}
}

func (p *CachePurger) Configured() bool {
	return p.zoneID != "" && p.apiToken != ""
}

func (p *CachePurger) PurgeURLs(ctx context.Context, urls []string) error {
	if !p.Configured() {
		return fmt.Errorf("cloudflare cache purge credentials not configured")
	}
	if len(urls) == 0 {
		return nil
	}

	payload, err := json.Marshal(map[string]any{"files": urls})
	if err != nil {
		return fmt.Errorf("failed to marshal purge payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/zones/%s/purge_cache", p.baseURL, p.zoneID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create purge request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare purge request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("cloudflare purge error (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// AssetDeletedPurgeSubscriber reacts to AssetDeletedEvent and purges public URLs from Cloudflare's edge cache.
type AssetDeletedPurgeSubscriber struct {
	purger port.CachePurger
}

func NewAssetDeletedPurgeSubscriber(purger port.CachePurger) *AssetDeletedPurgeSubscriber {
	return &AssetDeletedPurgeSubscriber{purger: purger}
}

func (s *AssetDeletedPurgeSubscriber) InterestedIn(eventName string) bool {
	return eventName == event.EventAssetDeleted
}

func (s *AssetDeletedPurgeSubscriber) Handle(ctx context.Context, evt event.Event) error {
	deleted, ok := evt.(event.AssetDeletedEvent)
	if !ok || deleted.Visibility != string(model.VisibilityPublic) {
		return nil
	}
	if s.purger == nil || !s.purger.Configured() {
		return nil
	}
	if err := s.purger.PurgeURLs(ctx, []string{deleted.PublicURL}); err != nil {
		log.Warn().Err(err).Str("url", deleted.PublicURL).Msg("Best-effort CDN cache purge failed after asset delete")
	}
	return nil
}
