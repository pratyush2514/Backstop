package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

type PauseController struct {
	mu              sync.RWMutex
	paused          bool
	reason          string
	updatedAt       time.Time
	file            string
	pauseBlocksSafe bool
}

type pauseStateFile struct {
	Paused    bool   `json:"paused"`
	Reason    string `json:"reason,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

func NewPauseController(file string, startPaused, pauseBlocksSafe bool) (*PauseController, error) {
	controller := &PauseController{
		paused:          startPaused,
		updatedAt:       time.Now().UTC(),
		file:            strings.TrimSpace(file),
		pauseBlocksSafe: pauseBlocksSafe,
	}
	if controller.file != "" {
		if err := controller.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if startPaused {
			if err := controller.SetPaused(true, "start-paused"); err != nil {
				return nil, err
			}
		}
	}
	return controller, nil
}

func (p *PauseController) SetPaused(paused bool, reason string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	p.paused = paused
	p.reason = strings.TrimSpace(reason)
	p.updatedAt = time.Now().UTC()
	p.mu.Unlock()
	return p.save()
}

func (p *PauseController) Status() map[string]any {
	if p == nil {
		return map[string]any{"paused": false}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return map[string]any{
		"paused":            p.paused,
		"reason":            p.reason,
		"updated_at":        p.updatedAt.Format(time.RFC3339Nano),
		"pause_blocks_safe": p.pauseBlocksSafe,
	}
}

func (p *PauseController) BlocksRisk(risk string) bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.paused {
		return false
	}
	return p.pauseBlocksSafe || risk != RiskSafe
}

func (p *PauseController) load() error {
	raw, err := os.ReadFile(p.file)
	if err != nil {
		return err
	}
	var state pauseStateFile
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	p.paused = state.Paused
	p.reason = state.Reason
	if state.UpdatedAt != "" {
		if ts, err := time.Parse(time.RFC3339Nano, state.UpdatedAt); err == nil {
			p.updatedAt = ts
		}
	}
	return nil
}

func (p *PauseController) save() error {
	if p == nil || p.file == "" {
		return nil
	}
	p.mu.RLock()
	state := pauseStateFile{
		Paused:    p.paused,
		Reason:    p.reason,
		UpdatedAt: p.updatedAt.Format(time.RFC3339Nano),
	}
	p.mu.RUnlock()
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.file, raw, 0o600)
}
