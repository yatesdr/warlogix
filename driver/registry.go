package driver

import (
	"fmt"

	"warlogix/config"
)

// Create creates a Driver for the given PLC configuration.
// The connection is not established until Connect() is called on the returned driver.
func Create(cfg *config.PLCConfig) (Driver, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}

	switch cfg.GetFamily() {
	case config.FamilyS7:
		return NewS7Adapter(cfg)
	case config.FamilyBeckhoff:
		return NewADSAdapter(cfg)
	case config.FamilyOmron:
		return NewOmronAdapter(cfg)
	case config.FamilyLogix, config.FamilyMicro800:
		return NewLogixAdapter(cfg)
	default:
		// Default to Logix for unknown families
		return NewLogixAdapter(cfg)
	}
}
