package backend

import (
	"github.com/adrianpriza-ai/alps/pack"
)

// Factory creates backend instances without circular dependencies
type Factory struct{}

// NewFactory creates a new backend factory
func NewFactory() *Factory {
	return &Factory{}
}

// DetectName returns the detected backend name (delegates to pack package)
func (f *Factory) DetectName() string {
	return pack.DetectName()
}

// DetectReal returns the detected backend (delegates to pack package)
func (f *Factory) DetectReal() *pack.Backend {
	return pack.Detect()
}

// NeedsSudo checks if a backend needs sudo (delegates to pack package)
func (f *Factory) NeedsSudo(backendName string) bool {
	return pack.NeedsSudo(backendName)
}

// CommandSupported checks if a command is supported (delegates to pack package)
func (f *Factory) CommandSupported(backendName, cmd string) bool {
	return pack.CommandSupported(backendName, cmd)
}
