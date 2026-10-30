package main

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SharedSwitch struct {
	Slot int    `json:"slot"`
	Name string `json:"name"`
}
type SharedSwitchSnapshot struct {
	Epoch   string         `json:"epoch"`
	Version uint64         `json:"version"`
	Pending []SharedSwitch `json:"pending"`
}
type sharedSwitchOperation struct {
	SharedSwitch
	token   uint64
	soundID string
	timer   *time.Timer
}
type SharedSwitches struct {
	mu      sync.Mutex
	epoch   string
	version uint64
	pending map[int]*sharedSwitchOperation
	timeout time.Duration
	publish func([]byte)
}

func newSharedSwitches(timeout time.Duration, publish func([]byte)) *SharedSwitches {
	return &SharedSwitches{epoch: strconv.FormatInt(time.Now().UnixNano(), 36), pending: make(map[int]*sharedSwitchOperation), timeout: timeout, publish: publish}
}
func (s *SharedSwitches) snapshotLocked() SharedSwitchSnapshot {
	snapshot := SharedSwitchSnapshot{Epoch: s.epoch, Version: s.version, Pending: make([]SharedSwitch, 0, len(s.pending))}
	for slot := 0; slot < soundboardSoundCount; slot++ {
		if pending := s.pending[slot]; pending != nil {
			snapshot.Pending = append(snapshot.Pending, pending.SharedSwitch)
		}
	}
	return snapshot
}
func switchStateFrame(snapshot SharedSwitchSnapshot) []byte {
	data, _ := json.Marshal(snapshot)
	return []byte(`<div id="switchstate" data-value="` + html.EscapeString(string(data)) + `"></div>`)
}
func (s *SharedSwitches) Frame() []byte {
	s.mu.Lock()
	snapshot := s.snapshotLocked()
	s.mu.Unlock()
	return switchStateFrame(snapshot)
}
func (s *SharedSwitches) Busy(slot int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending[slot] != nil
}
func (s *SharedSwitches) Begin(slot int, name string) (uint64, error) {
	s.mu.Lock()
	if slot < 0 || slot >= soundboardSoundCount {
		s.mu.Unlock()
		return 0, fmt.Errorf("invalid slot")
	}
	for _, pending := range s.pending {
		if pending.Slot == slot || pending.Name == name {
			s.mu.Unlock()
			return 0, fmt.Errorf("that slot or sound is already being uploaded")
		}
	}
	s.version++
	token := s.version
	s.pending[slot] = &sharedSwitchOperation{SharedSwitch: SharedSwitch{Slot: slot, Name: name}, token: token}
	snapshot := s.snapshotLocked()
	s.mu.Unlock()
	s.publish(switchStateFrame(snapshot))
	return token, nil
}
func (s *SharedSwitches) Remove(slot int, token uint64) {
	s.mu.Lock()
	pending := s.pending[slot]
	if pending == nil || pending.token != token {
		s.mu.Unlock()
		return
	}
	if pending.timer != nil {
		pending.timer.Stop()
	}
	delete(s.pending, slot)
	s.version++
	snapshot := s.snapshotLocked()
	s.mu.Unlock()
	s.publish(switchStateFrame(snapshot))
}
func (s *SharedSwitches) Accepted(slot int, token uint64, soundID string) {
	if soundID == "" {
		s.Remove(slot, token)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending[slot]
	if pending == nil || pending.token != token {
		return
	}
	pending.soundID = soundID
	pending.timer = time.AfterFunc(s.timeout, func() { s.Remove(slot, token) })
}

// Complete state is attached to the same frame that renders the new sound, so
// observers never see the loading overlay disappear before the replacement.
func (s *SharedSwitches) Resolve(updates []SoundboardSoundWithOrdinal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for slot, pending := range s.pending {
		if pending.soundID == "" {
			continue
		}
		for _, sound := range updates {
			if sound.ID == pending.soundID {
				if pending.timer != nil {
					pending.timer.Stop()
				}
				delete(s.pending, slot)
				s.version++
				break
			}
		}
	}
}

type trackedSwapHandler struct {
	state   *SharedSwitches
	board   func() [soundboardSoundCount]SoundboardSound
	execute func(swapSoundInput) (CreateSoundboardSoundResponse, error)
	refresh func([soundboardSoundCount]SoundboardSound)
}

func (h *trackedSwapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	defer func() { log.Printf("swap-sound total: %v", time.Since(started)) }()
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Use POST", 405)
		return
	}
	var input swapSoundInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input); err != nil {
		http.Error(w, "Invalid sound request", 400)
		return
	}
	h.HandleInput(w, input)
}
func (h *trackedSwapHandler) HandleInput(w http.ResponseWriter, input swapSoundInput) {
	if input.Add == (addSoundInput{}) {
		if _, err := h.execute(input); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(200)
		return
	}
	current := h.board()
	slot := -1
	if input.Slot != nil {
		slot = *input.Slot
	} else {
		for i, sound := range current {
			if (input.Delete.SoundID != "" && sound.ID == input.Delete.SoundID) || (input.Delete.SoundID == "" && sound.ID == "" && !h.state.Busy(i)) {
				slot = i
				break
			}
		}
	}
	if slot < 0 || slot >= soundboardSoundCount {
		http.Error(w, "No available target slot", 409)
		return
	}
	if current[slot].ID != input.Delete.SoundID {
		http.Error(w, "The target slot changed; refresh the board", 409)
		return
	}
	name := strings.TrimSuffix(path.Base(input.Add.SoundLocation), path.Ext(input.Add.SoundLocation))
	token, err := h.state.Begin(slot, name)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	accepted := false
	defer func() {
		if !accepted {
			h.state.Remove(slot, token)
		}
	}()
	result, err := h.execute(input)
	if err != nil {
		log.Printf("[error] swapping sound: %v", err)
		http.Error(w, err.Error(), 500)
		return
	}
	soundID := result.SoundID
	if soundID == "" {
		soundID = result.ID
	}
	h.state.Accepted(slot, token, soundID)
	accepted = true
	// The Gateway can beat the HTTP response. Queue a view containing that ID
	// after marking it accepted, rather than waiting for another Gateway event.
	current = h.board()
	for _, sound := range current {
		if soundID != "" && sound.ID == soundID {
			h.refresh(current)
			break
		}
	}
	w.WriteHeader(200)
}
