package widgets

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/scryner/my-streamdeck/internal/deckbutton"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"rafaelmartins.com/p/streamdeck"
)

const quiWidgetUpdateInterval = time.Minute

type QuiFetchFunc func(ctx context.Context, baseURL string, apiKey string) (quiSnapshot, error)
type QuiOpenFunc func(ctx context.Context, url string) error

type QuiWidgetOptions struct {
	Key        streamdeck.KeyID
	Size       int
	BaseURL    string
	APIKey     string
	Fetch      QuiFetchFunc
	Open       QuiOpenFunc
	HTTPClient *http.Client
}

type QuiWidget struct {
	key     streamdeck.KeyID
	baseURL string
	open    QuiOpenFunc
	source  *quiSource
}

type quiSource struct {
	size       int
	baseURL    string
	apiKey     string
	httpClient *http.Client
	fetch      QuiFetchFunc
	faces      quiFaces

	mu         sync.RWMutex
	lastFetch  time.Time
	cached     quiSnapshot
	hasData    bool
	signalLost bool
}

type quiFaces struct {
	header font.Face
	row    font.Face
}

type quiSnapshot struct {
	Downloading int
	Completed   int
	Seeding     int
}

type quiInstance struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	IsActive  bool   `json:"isActive"`
	Connected bool   `json:"connected"`
}

type quiCountsResponse struct {
	Counts struct {
		Status struct {
			Downloading int `json:"downloading"`
			Completed   int `json:"completed"`
			Seeding     int `json:"seeding"`
		} `json:"status"`
	} `json:"counts"`
}

func NewQuiWidget(options QuiWidgetOptions) (*QuiWidget, error) {
	if options.Size <= 0 {
		options.Size = DefaultClockWidgetSize
	}
	if options.BaseURL == "" {
		return nil, fmt.Errorf("qui base url is required")
	}
	if options.APIKey == "" {
		return nil, fmt.Errorf("qui api key is required")
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}

	faces, err := loadQuiFaces(options.Size)
	if err != nil {
		return nil, err
	}

	source := &quiSource{
		size:       options.Size,
		baseURL:    strings.TrimRight(options.BaseURL, "/"),
		apiKey:     options.APIKey,
		httpClient: options.HTTPClient,
		faces:      faces,
	}
	if options.Fetch != nil {
		source.fetch = options.Fetch
	} else {
		source.fetch = source.fetchSnapshot
	}

	return &QuiWidget{
		key:     options.Key,
		baseURL: source.baseURL,
		open:    resolveQuiOpenFunc(options.Open),
		source:  source,
	}, nil
}

func (w *QuiWidget) Button() deckbutton.Button {
	return deckbutton.Button{
		Key: w.key,
		OnPress: func(_ *streamdeck.Device, _ *streamdeck.Key) error {
			return w.open(context.Background(), w.baseURL+"/instances")
		},
		Animation: &deckbutton.Animation{
			Source:         w.source,
			UpdateInterval: quiWidgetUpdateInterval,
			Loop:           true,
		},
	}
}

func resolveQuiOpenFunc(fn QuiOpenFunc) QuiOpenFunc {
	if fn != nil {
		return fn
	}
	return openQuiURL
}

func openQuiURL(ctx context.Context, url string) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/open", url)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("open qui url: %w", err)
	}
	return nil
}

func (s *quiSource) Start(context.Context) error {
	return nil
}

func (s *quiSource) FrameAt(ctx context.Context, _ time.Duration) (image.Image, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	snapshot := s.cachedSnapshot()
	if s.shouldRefresh() {
		fresh, err := s.fetch(ctx, s.baseURL, s.apiKey)
		if err == nil {
			s.storeSnapshot(fresh)
			snapshot = fresh
		} else {
			s.markSignalLost()
		}
	}

	img := image.NewRGBA(image.Rect(0, 0, s.size, s.size))
	s.render(img, snapshot, s.hasSnapshot(), s.isSignalLost())
	return img, nil
}

func (s *quiSource) Duration() time.Duration {
	return 0
}

func (s *quiSource) Close() error {
	if s.httpClient != nil {
		s.httpClient.CloseIdleConnections()
	}

	s.mu.Lock()
	s.cached = quiSnapshot{}
	s.hasData = false
	s.lastFetch = time.Time{}
	s.signalLost = false
	s.mu.Unlock()

	return closeFaces(s.faces.header, s.faces.row)
}

func (s *quiSource) shouldRefresh() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.hasData || time.Since(s.lastFetch) >= quiWidgetUpdateInterval
}

func (s *quiSource) cachedSnapshot() quiSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cached
}

func (s *quiSource) hasSnapshot() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hasData
}

func (s *quiSource) storeSnapshot(snapshot quiSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached = snapshot
	s.hasData = true
	s.lastFetch = time.Now()
	s.signalLost = false
}

func (s *quiSource) markSignalLost() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFetch = time.Now()
	s.signalLost = true
}

func (s *quiSource) isSignalLost() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.signalLost
}

func (s *quiSource) fetchSnapshot(ctx context.Context, baseURL string, apiKey string) (quiSnapshot, error) {
	instancesReq, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/instances", nil)
	if err != nil {
		return quiSnapshot{}, fmt.Errorf("create qui instances request: %w", err)
	}
	instancesReq.Header.Set("X-API-Key", apiKey)
	instancesReq.Header.Set("Accept", "application/json")

	instancesResp, err := s.httpClient.Do(instancesReq)
	if err != nil {
		return quiSnapshot{}, fmt.Errorf("fetch qui instances: %w", err)
	}
	defer instancesResp.Body.Close()

	if instancesResp.StatusCode != http.StatusOK {
		return quiSnapshot{}, fmt.Errorf("fetch qui instances: unexpected status %s", instancesResp.Status)
	}

	var instances []quiInstance
	if err := decodeJSONBody(instancesResp.Body, &instances); err != nil {
		return quiSnapshot{}, fmt.Errorf("decode qui instances: %w", err)
	}

	var snapshot quiSnapshot
	for _, instance := range instances {
		if !instance.IsActive || !instance.Connected {
			continue
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/instances/%d/torrents?limit=1", baseURL, instance.ID), nil)
		if err != nil {
			return quiSnapshot{}, fmt.Errorf("create qui torrent request for instance %d: %w", instance.ID, err)
		}
		req.Header.Set("X-API-Key", apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return quiSnapshot{}, fmt.Errorf("fetch qui torrents for instance %d: %w", instance.ID, err)
		}

		var counts quiCountsResponse
		err = func() error {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status %s", resp.Status)
			}
			return decodeJSONBody(resp.Body, &counts)
		}()
		if err != nil {
			return quiSnapshot{}, fmt.Errorf("decode qui torrents for instance %d: %w", instance.ID, err)
		}

		snapshot.Downloading += counts.Counts.Status.Downloading
		snapshot.Completed += counts.Counts.Status.Completed
		snapshot.Seeding += counts.Counts.Status.Seeding
	}

	return snapshot, nil
}

func decodeJSONBody(body io.Reader, out interface{}) error {
	payload, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, out)
}

func loadQuiFaces(size int) (quiFaces, error) {
	scale := float64(size) / 72.0
	header, err := newFace(gobold.TTF, 7.5*scale)
	if err != nil {
		return quiFaces{}, fmt.Errorf("load qui header font: %w", err)
	}
	row, err := newFace(gobold.TTF, 9.5*scale)
	if err != nil {
		return quiFaces{}, fmt.Errorf("load qui row font: %w", err)
	}

	return quiFaces{
		header: header,
		row:    row,
	}, nil
}

func (s *quiSource) render(dst *image.RGBA, snapshot quiSnapshot, hasData bool, signalLost bool) {
	fillSolid(dst, color.RGBA{R: 15, G: 17, B: 20, A: 255})

	if signalLost {
		drawCenteredText(dst, s.faces.row, "NO SIGNAL", float64(s.size)/2, centeredTextBaselineY(s.faces.row, float64(s.size)/2), color.RGBA{R: 244, G: 246, B: 248, A: 255})
		return
	}

	rowHeight := float64(s.size) / 4
	for i := 1; i < 4; i++ {
		y := int(math.Round(float64(i) * rowHeight))
		for x := range s.size {
			dst.SetRGBA(x, y, color.RGBA{R: 76, G: 82, B: 90, A: 255})
		}
	}

	centerX := float64(s.size) / 2
	drawCenteredText(dst, s.faces.header, "QUI", centerX, centeredTextBaselineY(s.faces.header, rowHeight/2), color.RGBA{R: 227, G: 231, B: 236, A: 255})

	if !hasData {
		s.renderRow(dst, 1, "DOWN", "--", color.RGBA{R: 255, G: 186, B: 92, A: 255})
		s.renderRow(dst, 2, "DONE", "--", color.RGBA{R: 111, G: 226, B: 164, A: 255})
		s.renderRow(dst, 3, "SEED", "--", color.RGBA{R: 92, G: 214, B: 255, A: 255})
		return
	}

	s.renderRow(dst, 1, "DOWN", fmt.Sprintf("%d", snapshot.Downloading), color.RGBA{R: 255, G: 186, B: 92, A: 255})
	s.renderRow(dst, 2, "DONE", fmt.Sprintf("%d", snapshot.Completed), color.RGBA{R: 111, G: 226, B: 164, A: 255})
	s.renderRow(dst, 3, "SEED", fmt.Sprintf("%d", snapshot.Seeding), color.RGBA{R: 92, G: 214, B: 255, A: 255})
}

func (s *quiSource) renderRow(dst *image.RGBA, rowIndex int, label string, value string, labelColor color.RGBA) {
	rowHeight := float64(s.size) / 4
	centerY := (float64(rowIndex) * rowHeight) + (rowHeight / 2)

	labelWidth := measureTextWidth(s.faces.row, label)
	valueWidth := measureTextWidth(s.faces.row, value)
	gap := float64(scaledValue(s.size, 3))
	totalWidth := labelWidth + gap + valueWidth
	startX := (float64(s.size) - totalWidth) / 2
	baselineY := centeredTextBaselineY(s.faces.row, centerY)

	drawCenteredText(dst, s.faces.row, label, startX+(labelWidth/2), baselineY, labelColor)
	drawCenteredText(dst, s.faces.row, value, startX+labelWidth+gap+(valueWidth/2), baselineY, color.RGBA{R: 246, G: 247, B: 249, A: 255})
}
