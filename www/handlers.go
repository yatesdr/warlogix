package www

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"warlink/plcman"
	"warlink/tui"
)

// handleLoginPage renders the login page.
func (h *Handlers) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// If already logged in, redirect to home
	if username, _, ok := h.sessions.getUser(r); ok && username != "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	h.renderTemplate(w, "login.html", nil)
}

// handleLoginSubmit handles login form submission.
func (h *Handlers) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		h.renderTemplate(w, "login.html", map[string]interface{}{
			"Error": "Username and password are required",
		})
		return
	}

	// Find user in config
	user := h.managers.GetConfig().FindWebUser(username)
	if user == nil {
		h.renderTemplate(w, "login.html", map[string]interface{}{
			"Error": "Invalid username or password",
		})
		return
	}

	// Check password
	if !checkPassword(password, user.PasswordHash) {
		h.renderTemplate(w, "login.html", map[string]interface{}{
			"Error": "Invalid username or password",
		})
		return
	}

	// Set session
	if err := h.sessions.setUser(w, r, user.Username, user.Role); err != nil {
		h.renderTemplate(w, "login.html", map[string]interface{}{
			"Error": "Session error: " + err.Error(),
		})
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleLogout handles logout.
func (h *Handlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	h.sessions.clear(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// handleDashboard redirects to PLCs page.
func (h *Handlers) handleDashboard(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/plcs", http.StatusSeeOther)
}

// handlePLCsPage renders the PLCs page.
func (h *Handlers) handlePLCsPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "plcs"
	data["PLCs"] = h.getPLCsData()
	h.renderTemplate(w, "plcs.html", data)
}

// handleRepublisherPage renders the republisher page.
func (h *Handlers) handleRepublisherPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "republisher"
	data["PLCs"] = h.getRepublisherData()
	h.renderTemplate(w, "republisher.html", data)
}

// handleTagPacksPage renders the tag packs page.
func (h *Handlers) handleTagPacksPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "tagpacks"
	data["TagPacks"] = h.getTagPacksData()
	h.renderTemplate(w, "tagpacks.html", data)
}

// handleTriggersPage renders the triggers page.
func (h *Handlers) handleTriggersPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "triggers"
	data["Triggers"] = h.getTriggersData()
	h.renderTemplate(w, "triggers.html", data)
}

// handleRESTPage renders the REST API status page.
func (h *Handlers) handleRESTPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "rest"
	cfg := h.managers.GetConfig()
	data["APIEnabled"] = cfg.Web.API.Enabled
	data["Host"] = cfg.Web.Host
	data["Port"] = cfg.Web.Port
	h.renderTemplate(w, "rest.html", data)
}

// handleMQTTPage renders the MQTT brokers page.
func (h *Handlers) handleMQTTPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "mqtt"
	data["Brokers"] = h.getMQTTData()
	h.renderTemplate(w, "mqtt.html", data)
}

// handleValkeyPage renders the Valkey servers page.
func (h *Handlers) handleValkeyPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "valkey"
	data["Servers"] = h.getValkeyData()
	h.renderTemplate(w, "valkey.html", data)
}

// handleKafkaPage renders the Kafka clusters page.
func (h *Handlers) handleKafkaPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "kafka"
	data["Clusters"] = h.getKafkaData()
	h.renderTemplate(w, "kafka.html", data)
}

// handleDebugPage renders the debug log page.
func (h *Handlers) handleDebugPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "debug"
	data["Logs"] = h.getDebugLogs()
	data["LogEntries"] = h.getDebugLogEntries()
	h.renderTemplate(w, "debug.html", data)
}

// Data helper functions

// PLCData holds PLC display data.
type PLCData struct {
	Name        string
	Address     string
	Slot        int
	Family      string
	Status      string
	StatusClass string
	ProductName string
	SerialNumber string
	Vendor      string
	Error       string
	Enabled     bool
	TagCount    int
	PollRate    string
	// Family-specific fields
	AmsNetId    string
	AmsPort     int
	Protocol    string
	FinsPort    int
	FinsNetwork int
	FinsNode    int
	FinsUnit    int
}

func (h *Handlers) getPLCsData() []PLCData {
	manager := h.managers.GetPLCMan()
	plcs := manager.ListPLCs()
	result := make([]PLCData, 0, len(plcs))

	for _, plc := range plcs {
		status := plc.GetStatus()
		statusClass := "status-disconnected"
		switch status {
		case plcman.StatusConnected:
			statusClass = "status-connected"
		case plcman.StatusConnecting:
			statusClass = "status-connecting"
		case plcman.StatusError:
			statusClass = "status-error"
		}

		pd := PLCData{
			Name:        plc.Config.Name,
			Address:     plc.Config.Address,
			Slot:        int(plc.Config.Slot),
			Family:      plc.Config.GetFamily().String(),
			Status:      status.String(),
			StatusClass: statusClass,
			Enabled:     plc.Config.Enabled,
			TagCount:    len(plc.Config.Tags),
			// Beckhoff fields
			AmsNetId:    plc.Config.AmsNetId,
			AmsPort:     int(plc.Config.AmsPort),
			// Omron fields
			Protocol:    plc.Config.Protocol,
			FinsPort:    plc.Config.FinsPort,
			FinsNetwork: int(plc.Config.FinsNetwork),
			FinsNode:    int(plc.Config.FinsNode),
			FinsUnit:    int(plc.Config.FinsUnit),
		}

		if plc.Config.PollRate > 0 {
			pd.PollRate = plc.Config.PollRate.String()
		}

		if info := plc.GetDeviceInfo(); info != nil {
			pd.ProductName = info.Model
			pd.SerialNumber = info.SerialNumber
			pd.Vendor = info.Vendor
		}
		if err := plc.GetError(); err != nil {
			pd.Error = err.Error()
		}

		result = append(result, pd)
	}

	// Sort by name for stable ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// RepublisherPLC holds PLC data with sections for the republisher view.
type RepublisherPLC struct {
	Name        string
	Status      string
	StatusClass string
	LastPoll    string // Formatted timestamp of last poll
	Sections    []RepublisherSection
}

// RepublisherSection holds a group of tags (Controller, Program, etc.)
type RepublisherSection struct {
	Name string // "Controller" or program name like "MainProgram"
	Tags []RepublisherTag
}

// PublishedChild holds info about a child member that is independently published.
type PublishedChild struct {
	Enabled  bool `json:"enabled"`
	Writable bool `json:"writable"`
}

// RepublisherTag holds tag display data for the republisher view.
type RepublisherTag struct {
	Name              string
	Alias             string
	Type              string
	Value             string
	JSONValue         string                    // Full JSON representation
	Writable          bool
	IsStruct          bool
	FieldCount        int                       // Number of fields for struct types
	DisplayName       string                    // Short name for tree display
	TreeDisplay       string                    // Full display string: Name (Type) [N fields]
	Enabled           bool                      // Whether tag is monitored/enabled
	HasIgnores        bool                      // Whether tag has IgnoreChanges entries
	IgnoreCount       int                       // Number of ignored members
	IgnoreList        []string                  // List of ignored member names
	LastPoll          string                    // Formatted timestamp of last poll
	LastChanged       string                    // Formatted timestamp of last value change
	PublishedChildren map[string]PublishedChild // Map of child paths to their published status
}

func (h *Handlers) getRepublisherData() []RepublisherPLC {
	manager := h.managers.GetPLCMan()
	plcs := manager.ListPLCs()
	result := make([]RepublisherPLC, 0, len(plcs))

	for _, plc := range plcs {
		status := plc.GetStatus()
		statusClass := "status-disconnected"
		switch status {
		case plcman.StatusConnected:
			statusClass = "status-connected"
		case plcman.StatusConnecting:
			statusClass = "status-connecting"
		case plcman.StatusError:
			statusClass = "status-error"
		}

		lastPollStr := ""
		if !plc.LastPoll.IsZero() {
			lastPollStr = plc.LastPoll.Format("2006-01-02 15:04:05")
		}

		rp := RepublisherPLC{
			Name:        plc.Config.Name,
			Status:      status.String(),
			StatusClass: statusClass,
			LastPoll:    lastPollStr,
			Sections:    make([]RepublisherSection, 0),
		}

		// Build config lookup map for enabled/writable/ignored settings
		configMap := make(map[string]*struct {
			Alias         string
			Enabled       bool
			Writable      bool
			IgnoreChanges []string
		})
		// Also build a map of parent tag -> published children
		childTagsMap := make(map[string]map[string]PublishedChild)
		for i := range plc.Config.Tags {
			sel := &plc.Config.Tags[i]
			configMap[sel.Name] = &struct {
				Alias         string
				Enabled       bool
				Writable      bool
				IgnoreChanges []string
			}{
				Alias:         sel.Alias,
				Enabled:       sel.Enabled,
				Writable:      sel.Writable,
				IgnoreChanges: sel.IgnoreChanges,
			}

			// Check if this is a child tag (contains a dot after the parent name)
			// e.g., "Robot1.Position.X" -> parent="Robot1", path="Position.X"
			if idx := strings.Index(sel.Name, "."); idx > 0 {
				parentName := sel.Name[:idx]
				childPath := sel.Name[idx+1:]
				if childTagsMap[parentName] == nil {
					childTagsMap[parentName] = make(map[string]PublishedChild)
				}
				childTagsMap[parentName][childPath] = PublishedChild{
					Enabled:  sel.Enabled,
					Writable: sel.Writable,
				}
			}
		}

		// Get runtime data
		tags := plc.GetTags()
		programs := plc.GetPrograms()
		values := plc.GetValues()
		var lastChanged map[string]time.Time // not tracked yet

		// Build program prefix set for Beckhoff-style grouping
		programPrefixes := make(map[string]bool)
		for _, prog := range programs {
			programPrefixes[prog+"."] = true
		}

		// Organize tags by section (Controller vs Program)
		controllerTags := make([]RepublisherTag, 0)
		programTags := make(map[string][]RepublisherTag)

		for _, tag := range tags {
			rt := h.buildRepublisherTag(tag.Name, tag.TypeName, configMap, childTagsMap, values, lastChanged, lastPollStr)

			if strings.HasPrefix(tag.Name, "Program:") {
				// Logix-style: "Program:MainProgram.tagname"
				rest := strings.TrimPrefix(tag.Name, "Program:")
				if idx := strings.Index(rest, "."); idx > 0 {
					progName := rest[:idx]
					programTags[progName] = append(programTags[progName], rt)
				} else {
					controllerTags = append(controllerTags, rt)
				}
			} else {
				// Check for Beckhoff-style: "MAIN.tagname", "GVL.globalvar"
				matched := false
				for prefix := range programPrefixes {
					if strings.HasPrefix(tag.Name, prefix) {
						progName := strings.TrimSuffix(prefix, ".")
						programTags[progName] = append(programTags[progName], rt)
						matched = true
						break
					}
				}
				if !matched {
					controllerTags = append(controllerTags, rt)
				}
			}
		}

		// Add Controller section if it has tags
		if len(controllerTags) > 0 {
			sort.Slice(controllerTags, func(i, j int) bool {
				return controllerTags[i].Name < controllerTags[j].Name
			})
			rp.Sections = append(rp.Sections, RepublisherSection{
				Name: "Controller",
				Tags: controllerTags,
			})
		}

		// Add Program sections
		sort.Strings(programs)
		for _, prog := range programs {
			if tags, ok := programTags[prog]; ok && len(tags) > 0 {
				sort.Slice(tags, func(i, j int) bool {
					return tags[i].Name < tags[j].Name
				})
				rp.Sections = append(rp.Sections, RepublisherSection{
					Name: prog,
					Tags: tags,
				})
			}
		}

		result = append(result, rp)
	}

	// Sort PLCs by name
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// buildRepublisherTag creates a RepublisherTag from runtime and config data.
func (h *Handlers) buildRepublisherTag(
	tagName, typeName string,
	configMap map[string]*struct {
		Alias         string
		Enabled       bool
		Writable      bool
		IgnoreChanges []string
	},
	childTagsMap map[string]map[string]PublishedChild,
	values map[string]*plcman.TagValue,
	lastChanged map[string]time.Time,
	lastPollStr string,
) RepublisherTag {
	rt := RepublisherTag{
		Name:              tagName,
		DisplayName:       getDisplayName(tagName),
		Type:              typeName,
		LastPoll:          lastPollStr,
		PublishedChildren: make(map[string]PublishedChild),
	}

	// Get last changed timestamp for this tag
	if t, ok := lastChanged[tagName]; ok && !t.IsZero() {
		rt.LastChanged = t.Format("2006-01-02 15:04:05")
	}

	// Get config settings if available
	if cfg, ok := configMap[tagName]; ok {
		rt.Alias = cfg.Alias
		rt.Enabled = cfg.Enabled
		rt.Writable = cfg.Writable
		rt.HasIgnores = len(cfg.IgnoreChanges) > 0
		rt.IgnoreCount = len(cfg.IgnoreChanges)
		rt.IgnoreList = cfg.IgnoreChanges
	}

	// Get published children for this tag
	if children, ok := childTagsMap[tagName]; ok {
		rt.PublishedChildren = children
	}

	// Get value data if available
	if v, ok := values[tagName]; ok && v != nil {
		goVal := v.GoValue()
		rt.Value = formatTagValue(goVal)
		rt.IsStruct = isStructType(rt.Type)

		// Count fields for struct types
		if m, ok := goVal.(map[string]interface{}); ok {
			rt.FieldCount = len(m)
		}

		// Create compact JSON representation
		if jsonBytes, err := json.Marshal(goVal); err == nil {
			rt.JSONValue = string(jsonBytes)
		} else {
			rt.JSONValue = fmt.Sprintf("%v", goVal)
		}
	}

	// Build tree display string
	rt.TreeDisplay = buildTreeDisplay(rt)

	return rt
}

// buildTreeDisplay creates the display string: "DisplayName (Type) [N fields]" or "DisplayName (Type): value"
func buildTreeDisplay(rt RepublisherTag) string {
	display := rt.DisplayName
	if rt.Alias != "" {
		display = rt.Alias
	}

	if rt.Type != "" {
		display += " (" + rt.Type + ")"
	}

	if rt.FieldCount > 0 {
		display += fmt.Sprintf(" [%d fields]", rt.FieldCount)
	}

	return display
}

// getDisplayName returns the short display name for a tag (last component).
func getDisplayName(name string) string {
	// Handle Program:tag format
	if idx := strings.LastIndex(name, ":"); idx >= 0 {
		name = name[idx+1:]
	}
	// Handle nested.member format
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}

// formatTagValue formats a tag value for compact display.
func formatTagValue(v interface{}) string {
	if v == nil {
		return "-"
	}
	switch val := v.(type) {
	case map[string]interface{}:
		return fmt.Sprintf("{%d fields}", len(val))
	case []interface{}:
		if len(val) > 3 {
			return fmt.Sprintf("[%d items]", len(val))
		}
		return fmt.Sprintf("%v", val)
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%.0f", val)
		}
		return fmt.Sprintf("%.4g", val)
	case float32:
		if val == float32(int32(val)) {
			return fmt.Sprintf("%.0f", val)
		}
		return fmt.Sprintf("%.4g", val)
	case string:
		if len(val) > 30 {
			return val[:27] + "..."
		}
		return val
	default:
		s := fmt.Sprintf("%v", val)
		if len(s) > 30 {
			return s[:27] + "..."
		}
		return s
	}
}

// isStructType checks if a type name indicates a struct/UDT.
func isStructType(typeName string) bool {
	// UDTs typically don't match basic type names
	basicTypes := map[string]bool{
		"BOOL": true, "SINT": true, "INT": true, "DINT": true, "LINT": true,
		"USINT": true, "UINT": true, "UDINT": true, "ULINT": true,
		"REAL": true, "LREAL": true, "STRING": true, "BYTE": true,
		"WORD": true, "DWORD": true, "LWORD": true, "TIME": true,
		"bool": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"uint8": true, "uint16": true, "uint32": true, "uint64": true,
		"float32": true, "float64": true, "string": true,
	}
	return !basicTypes[typeName] && typeName != ""
}

// MQTTData holds MQTT broker display data.
type MQTTData struct {
	Name        string
	Broker      string
	Port        int
	UseTLS      bool
	Status      string
	StatusClass string
	Enabled     bool
}

func (h *Handlers) getMQTTData() []MQTTData {
	cfg := h.managers.GetConfig()
	mqttMgr := h.managers.GetMQTTMgr()
	result := make([]MQTTData, 0, len(cfg.MQTT))

	for _, mqttCfg := range cfg.MQTT {
		statusClass := "status-disconnected"
		status := "Stopped"

		// Get runtime info from publisher
		pub := mqttMgr.Get(mqttCfg.Name)
		if pub != nil && pub.IsRunning() {
			statusClass = "status-connected"
			status = "Connected"
		}

		result = append(result, MQTTData{
			Name:        mqttCfg.Name,
			Broker:      mqttCfg.Broker,
			Port:        mqttCfg.Port,
			UseTLS:      mqttCfg.UseTLS,
			Status:      status,
			StatusClass: statusClass,
			Enabled:     mqttCfg.Enabled,
		})
	}

	// Sort by name for stable ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// ValkeyData holds Valkey server display data.
type ValkeyData struct {
	Name            string
	Address         string
	Database        int
	UseTLS          bool
	EnableWriteback bool
	Status          string
	StatusClass     string
	Enabled         bool
}

func (h *Handlers) getValkeyData() []ValkeyData {
	cfg := h.managers.GetConfig()
	valkeyMgr := h.managers.GetValkeyMgr()
	result := make([]ValkeyData, 0, len(cfg.Valkey))

	for _, valkeyCfg := range cfg.Valkey {
		statusClass := "status-disconnected"
		status := "Stopped"

		// Get runtime info from publisher
		pub := valkeyMgr.Get(valkeyCfg.Name)
		if pub != nil && pub.IsRunning() {
			statusClass = "status-connected"
			status = "Connected"
		}

		result = append(result, ValkeyData{
			Name:            valkeyCfg.Name,
			Address:         valkeyCfg.Address,
			Database:        valkeyCfg.Database,
			UseTLS:          valkeyCfg.UseTLS,
			EnableWriteback: valkeyCfg.EnableWriteback,
			Status:          status,
			StatusClass:     statusClass,
			Enabled:         valkeyCfg.Enabled,
		})
	}

	// Sort by name for stable ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// KafkaData holds Kafka cluster display data.
type KafkaData struct {
	Name        string
	Brokers     string
	Status      string
	StatusClass string
	Enabled     bool
}

func (h *Handlers) getKafkaData() []KafkaData {
	cfg := h.managers.GetConfig()
	kafkaMgr := h.managers.GetKafkaMgr()
	result := make([]KafkaData, 0, len(cfg.Kafka))

	for _, kafkaCfg := range cfg.Kafka {
		statusClass := "status-disconnected"
		status := "Stopped"

		// Get runtime info from producer
		producer := kafkaMgr.GetProducer(kafkaCfg.Name)
		if producer != nil {
			pStatus := producer.GetStatus()
			switch pStatus {
			case 2: // Connected
				statusClass = "status-connected"
				status = "Connected"
			case 1: // Connecting
				statusClass = "status-connecting"
				status = "Connecting"
			case 3: // Error
				statusClass = "status-error"
				status = "Error"
			}
		}

		result = append(result, KafkaData{
			Name:        kafkaCfg.Name,
			Brokers:     joinBrokers(kafkaCfg.Brokers),
			Status:      status,
			StatusClass: statusClass,
			Enabled:     kafkaCfg.Enabled,
		})
	}

	// Sort by name for stable ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

func joinBrokers(brokers []string) string {
	if len(brokers) == 0 {
		return ""
	}
	if len(brokers) == 1 {
		return brokers[0]
	}
	result := brokers[0]
	for i := 1; i < len(brokers) && i < 3; i++ {
		result += ", " + brokers[i]
	}
	if len(brokers) > 3 {
		result += "..."
	}
	return result
}

// TagPackData holds TagPack display data.
type TagPackData struct {
	Name    string
	Enabled bool
	Members []TagPackMemberData
	MQTT    bool
	Kafka   bool
	Valkey  bool
}

// TagPackMemberData holds display data for a TagPack member.
type TagPackMemberData struct {
	PLC           string
	Tag           string
	IgnoreChanges bool
}

func (h *Handlers) getTagPacksData() []TagPackData {
	cfg := h.managers.GetConfig()
	result := make([]TagPackData, 0, len(cfg.TagPacks))

	for _, pack := range cfg.TagPacks {
		members := make([]TagPackMemberData, len(pack.Members))
		for i, m := range pack.Members {
			members[i] = TagPackMemberData{
				PLC:           m.PLC,
				Tag:           m.Tag,
				IgnoreChanges: m.IgnoreChanges,
			}
		}
		result = append(result, TagPackData{
			Name:    pack.Name,
			Enabled: pack.Enabled,
			Members: members,
			MQTT:    pack.MQTTEnabled,
			Kafka:   pack.KafkaEnabled,
			Valkey:  pack.ValkeyEnabled,
		})
	}

	// Sort by name for stable ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// TriggerData holds Trigger display data.
type TriggerData struct {
	Name        string
	PLC         string
	TriggerTag  string
	Operator    string
	Value       string
	Status      string
	StatusClass string
	Enabled     bool
	Tags        []string
}

func (h *Handlers) getTriggersData() []TriggerData {
	cfg := h.managers.GetConfig()
	triggerMgr := h.managers.GetTriggerMgr()
	result := make([]TriggerData, 0, len(cfg.Triggers))

	for _, triggerCfg := range cfg.Triggers {
		statusClass := "status-disconnected"
		status := "Stopped"

		// Get runtime status
		tStatus, _, _, _ := triggerMgr.GetTriggerStatus(triggerCfg.Name)
		switch tStatus {
		case 1: // StatusArmed
			statusClass = "status-connected"
			status = "Armed"
		case 2: // StatusFiring
			statusClass = "status-connecting"
			status = "Firing"
		}

		result = append(result, TriggerData{
			Name:        triggerCfg.Name,
			PLC:         triggerCfg.PLC,
			TriggerTag:  triggerCfg.TriggerTag,
			Operator:    triggerCfg.Condition.Operator,
			Value:       fmt.Sprintf("%v", triggerCfg.Condition.Value),
			Status:      status,
			StatusClass: statusClass,
			Enabled:     triggerCfg.Enabled,
			Tags:        triggerCfg.Tags,
		})
	}

	// Sort by name for stable ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

func (h *Handlers) getDebugLogs() []string {
	store := tui.GetDebugStore()
	if store == nil {
		return nil
	}
	messages := store.GetMessages()
	lines := make([]string, len(messages))
	for i, msg := range messages {
		ts := msg.Timestamp.Format("15:04:05")
		if msg.Level != "" {
			lines[i] = fmt.Sprintf("[%s] [%s] %s", ts, msg.Level, msg.Message)
		} else {
			lines[i] = fmt.Sprintf("[%s] %s", ts, msg.Message)
		}
	}
	return lines
}

// DebugLogEntry holds structured debug log data for templates.
type DebugLogEntry struct {
	Timestamp string
	Level     string
	Message   string
}

func (h *Handlers) getDebugLogEntries() []DebugLogEntry {
	store := tui.GetDebugStore()
	if store == nil {
		return nil
	}
	messages := store.GetMessages()
	entries := make([]DebugLogEntry, len(messages))
	for i, msg := range messages {
		entries[i] = DebugLogEntry{
			Timestamp: msg.Timestamp.Format("15:04:05"),
			Level:     msg.Level,
			Message:   msg.Message,
		}
	}
	return entries
}
