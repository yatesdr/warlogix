package engine

import (
	"fmt"
	"strings"

	"warlink/config"
)

// UpdateTag updates a tag's configuration (enabled, writable, ignore list).
// Auto-creates the tag entry if it doesn't exist.
func (e *Engine) UpdateTag(plcName, tagName string, req TagUpdateRequest) error {
	e.cfg.Lock()
	plcCfg := e.cfg.FindPLC(plcName)
	if plcCfg == nil {
		e.cfg.Unlock()
		return fmt.Errorf("%w: PLC '%s'", ErrNotFound, plcName)
	}

	tagFound := false
	for i := range plcCfg.Tags {
		if plcCfg.Tags[i].Name == tagName {
			if req.Enabled != nil {
				plcCfg.Tags[i].Enabled = *req.Enabled
			}
			if req.Writable != nil {
				plcCfg.Tags[i].Writable = *req.Writable
			}
			if req.NoREST != nil {
				plcCfg.Tags[i].NoREST = *req.NoREST
			}
			if req.NoMQTT != nil {
				plcCfg.Tags[i].NoMQTT = *req.NoMQTT
			}
			if req.NoKafka != nil {
				plcCfg.Tags[i].NoKafka = *req.NoKafka
			}
			if req.NoValkey != nil {
				plcCfg.Tags[i].NoValkey = *req.NoValkey
			}
			if len(req.AddIgnore) > 0 {
				for _, path := range req.AddIgnore {
					found := false
					for _, existing := range plcCfg.Tags[i].IgnoreChanges {
						if existing == path {
							found = true
							break
						}
					}
					if !found {
						plcCfg.Tags[i].IgnoreChanges = append(plcCfg.Tags[i].IgnoreChanges, path)
					}
				}
			}
			if len(req.RemoveIgnore) > 0 {
				newList := make([]string, 0)
				for _, existing := range plcCfg.Tags[i].IgnoreChanges {
					keep := true
					for _, remove := range req.RemoveIgnore {
						if existing == remove {
							keep = false
							break
						}
					}
					if keep {
						newList = append(newList, existing)
					}
				}
				plcCfg.Tags[i].IgnoreChanges = newList
			}
			tagFound = true
			break
		}
	}

	if !tagFound {
		newTag := config.TagSelection{Name: tagName}
		if req.Enabled != nil {
			newTag.Enabled = *req.Enabled
		}
		if req.Writable != nil {
			newTag.Writable = *req.Writable
		}
		if len(req.AddIgnore) > 0 {
			newTag.IgnoreChanges = req.AddIgnore
		}
		plcCfg.Tags = append(plcCfg.Tags, newTag)
	}

	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	if !tagFound {
		e.plcMan.RefreshManualTags(plcName)
	}

	e.emit(EventTagUpdated, TagEvent{PLCName: plcName, TagName: tagName})
	return nil
}

// CreateOrUpdateTag creates or fully replaces a tag entry (used for adding child tags).
// Returns true if a new tag was created (vs updating existing).
func (e *Engine) CreateOrUpdateTag(plcName, tagName string, req TagCreateOrUpdateRequest) (created bool, err error) {
	e.cfg.Lock()
	plcCfg := e.cfg.FindPLC(plcName)
	if plcCfg == nil {
		e.cfg.Unlock()
		return false, fmt.Errorf("%w: PLC '%s'", ErrNotFound, plcName)
	}

	tagFound := false
	for i := range plcCfg.Tags {
		if plcCfg.Tags[i].Name == tagName {
			plcCfg.Tags[i].Enabled = req.Enabled
			plcCfg.Tags[i].Writable = req.Writable
			plcCfg.Tags[i].DataType = req.DataType
			plcCfg.Tags[i].Alias = req.Alias
			tagFound = true
			break
		}
	}

	if !tagFound {
		plcCfg.Tags = append(plcCfg.Tags, config.TagSelection{
			Name:     tagName,
			DataType: req.DataType,
			Alias:    req.Alias,
			Enabled:  req.Enabled,
			Writable: req.Writable,
		})
	}

	if err := e.saveConfig(); err != nil {
		return false, fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	if !tagFound {
		e.plcMan.RefreshManualTags(plcName)
	}

	if tagFound {
		e.emit(EventTagUpdated, TagEvent{PLCName: plcName, TagName: tagName})
	} else {
		e.emit(EventTagCreated, TagEvent{PLCName: plcName, TagName: tagName})
	}
	return !tagFound, nil
}

// DeleteTag removes a tag from a PLC's configuration.
func (e *Engine) DeleteTag(plcName, tagName string) error {
	e.cfg.Lock()
	plcCfg := e.cfg.FindPLC(plcName)
	if plcCfg == nil {
		e.cfg.Unlock()
		return fmt.Errorf("%w: PLC '%s'", ErrNotFound, plcName)
	}

	found := false
	for i, tag := range plcCfg.Tags {
		if tag.Name == tagName {
			plcCfg.Tags = append(plcCfg.Tags[:i], plcCfg.Tags[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		e.cfg.Unlock()
		return fmt.Errorf("%w: tag '%s'", ErrNotFound, tagName)
	}

	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.plcMan.RefreshManualTags(plcName)
	e.emit(EventTagDeleted, TagEvent{PLCName: plcName, TagName: tagName})
	return nil
}

// IsChildTag returns true if the tag name contains a dot separator.
func IsChildTag(tagName string) bool {
	return strings.Contains(tagName, ".")
}

// WriteTag writes a value to a tag on a connected PLC.
func (e *Engine) WriteTag(plcName, tagName string, value interface{}) error {
	return e.plcMan.WriteTag(plcName, tagName, value)
}

// ReadTag reads a tag value from a PLC.
func (e *Engine) ReadTag(plcName, tagName string) (interface{}, error) {
	return e.plcMan.ReadTag(plcName, tagName)
}
