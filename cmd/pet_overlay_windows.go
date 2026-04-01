//go:build windows

package cmd

import "github.com/maorbril/clauder/internal/store"

const petOverlayLines = 0

type petOverlay struct{}

func newPetOverlay(_ store.Store, _ string) *petOverlay { return &petOverlay{} }
func (p *petOverlay) Start()                            {}
func (p *petOverlay) Stop()                             {}
func (p *petOverlay) HandleResize()                     {}
