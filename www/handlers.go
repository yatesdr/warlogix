package www

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"warlink/config"
	"warlink/kafka"
	"warlink/plcman"
	"warlink/rule"
	"warlink/tui"
)

// handleLoginPage renders the login page.
func (h *Handlers) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// If no users exist, redirect to setup
	if len(h.managers.GetConfig().Web.UI.Users) == 0 {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}

	// If already logged in, redirect to home
	if username, _, ok := h.sessions.getUser(r); ok && username != "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Clear any stale session cookie so login starts fresh.
	// This handles the case where a session secret was rotated (e.g. after
	// config reset) and the browser still holds a cookie signed with the old key.
	h.sessions.clear(w, r)

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

	// Redirect to password change if required
	if user.MustChangePassword {
		http.Redirect(w, r, "/change-password", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleLogout handles logout.
func (h *Handlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	h.sessions.clear(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// handleChangePasswordPage renders the change password form.
func (h *Handlers) handleChangePasswordPage(w http.ResponseWriter, r *http.Request) {
	username, _, ok := h.sessions.getUser(r)
	if !ok || username == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.renderTemplate(w, "change_password.html", map[string]interface{}{
		"Username": username,
	})
}

// handleChangePasswordSubmit handles the change password form submission.
func (h *Handlers) handleChangePasswordSubmit(w http.ResponseWriter, r *http.Request) {
	username, _, ok := h.sessions.getUser(r)
	if !ok || username == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	renderError := func(msg string) {
		h.renderTemplate(w, "change_password.html", map[string]interface{}{
			"Username": username,
			"Error":    msg,
		})
	}

	if currentPassword == "" || newPassword == "" || confirmPassword == "" {
		renderError("All fields are required")
		return
	}

	if newPassword != confirmPassword {
		renderError("New passwords do not match")
		return
	}

	if newPassword == "admin" {
		renderError("Password cannot be 'admin'")
		return
	}

	if len(newPassword) < 4 {
		renderError("Password must be at least 4 characters")
		return
	}

	cfg := h.managers.GetConfig()
	user := cfg.FindWebUser(username)
	if user == nil {
		renderError("User not found")
		return
	}

	if !checkPassword(currentPassword, user.PasswordHash) {
		renderError("Current password is incorrect")
		return
	}

	hash, err := hashPassword(newPassword)
	if err != nil {
		renderError("Failed to hash password")
		return
	}

	cfg.Lock()
	user.PasswordHash = hash
	user.MustChangePassword = false
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		renderError("Failed to save: " + err.Error())
		return
	}

	// Clear unsecured deadline since password was changed
	if h.webServer != nil {
		h.webServer.ClearUnsecuredDeadline()
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleSetupPage renders the first-run setup page.
func (h *Handlers) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	// If users already exist, redirect to login
	if len(h.managers.GetConfig().Web.UI.Users) > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.renderTemplate(w, "setup.html", nil)
}

// handleSetupSubmit handles the first-run setup form submission.
func (h *Handlers) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	cfg := h.managers.GetConfig()

	// Guard against race/replay — if users already exist, reject
	if len(cfg.Web.UI.Users) > 0 {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	renderError := func(msg string) {
		h.renderTemplate(w, "setup.html", map[string]interface{}{
			"Error":    msg,
			"Username": username,
		})
	}

	if username == "" {
		renderError("Username is required")
		return
	}

	if len(password) < 4 {
		renderError("Password must be at least 4 characters")
		return
	}

	if password != confirmPassword {
		renderError("Passwords do not match")
		return
	}

	hash, err := hashPassword(password)
	if err != nil {
		renderError("Failed to hash password")
		return
	}

	cfg.Lock()
	cfg.AddWebUser(config.WebUser{
		Username:           username,
		PasswordHash:       hash,
		Role:               config.RoleAdmin,
		MustChangePassword: false,
	})

	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		renderError("Failed to save: " + err.Error())
		return
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// handleSetupNamespacePage renders the mandatory namespace setup page.
func (h *Handlers) handleSetupNamespacePage(w http.ResponseWriter, r *http.Request) {
	username, role, ok := h.sessions.getUser(r)
	if !ok || username == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	cfg := h.managers.GetConfig()
	if cfg.Namespace != "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	h.renderTemplate(w, "setup_namespace.html", map[string]interface{}{
		"IsAdmin": isAdmin(role),
	})
}

// handleSetupNamespaceSubmit handles the namespace setup form submission.
func (h *Handlers) handleSetupNamespaceSubmit(w http.ResponseWriter, r *http.Request) {
	_, role, ok := h.sessions.getUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !isAdmin(role) {
		http.Error(w, "Forbidden: Admin access required", http.StatusForbidden)
		return
	}

	namespace := strings.TrimSpace(r.FormValue("namespace"))

	if !config.IsValidNamespace(namespace) {
		h.renderTemplate(w, "setup_namespace.html", map[string]interface{}{
			"IsAdmin":   true,
			"Error":     "Invalid namespace. Use lowercase letters, numbers, and hyphens (e.g. plant-floor-1).",
			"Namespace": namespace,
		})
		return
	}

	cfg := h.managers.GetConfig()
	cfg.Lock()
	cfg.Namespace = namespace
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		h.renderTemplate(w, "setup_namespace.html", map[string]interface{}{
			"IsAdmin":   true,
			"Error":     "Failed to save: " + err.Error(),
			"Namespace": namespace,
		})
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
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

// handleRulesPage renders the rules page.
func (h *Handlers) handleRulesPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "rules"
	data["Rules"] = h.getRulesData()

	// Provide PLC/MQTT/Kafka names for condition/action dropdowns
	cfg := h.managers.GetConfig()
	plcNames := make([]string, 0, len(cfg.PLCs))
	for _, p := range cfg.PLCs {
		plcNames = append(plcNames, p.Name)
	}
	data["PLCNames"] = plcNames

	mqttNames := make([]string, 0, len(cfg.MQTT))
	for _, m := range cfg.MQTT {
		mqttNames = append(mqttNames, m.Name)
	}
	data["MQTTNames"] = mqttNames

	kafkaNames := make([]string, 0, len(cfg.Kafka))
	for _, k := range cfg.Kafka {
		kafkaNames = append(kafkaNames, k.Name)
	}
	data["KafkaNames"] = kafkaNames

	tagPackNames := make([]string, 0, len(cfg.TagPacks))
	for _, tp := range cfg.TagPacks {
		tagPackNames = append(tagPackNames, tp.Name)
	}
	data["TagPackNames"] = tagPackNames

	h.renderTemplate(w, "rules.html", data)
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
	ConnectionMode string
	ConnectionPath string
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
			Name:           plc.Config.Name,
			Address:        plc.Config.Address,
			Slot:           int(plc.Config.Slot),
			Family:         plc.Config.GetFamily().String(),
			Status:         status.String(),
			StatusClass:    statusClass,
			Enabled:        plc.Config.Enabled,
			ConnectionPath: plc.Config.ConnectionPath,
			TagCount:       len(plc.GetTags()),
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

		pd.ConnectionMode = plc.GetConnectionMode()

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
	Name              string
	Address           string
	Family            string
	ProductName       string
	ConnectionMode    string
	Status            string
	StatusClass       string
	LastPoll          string // Formatted timestamp of last poll
	Sections          []RepublisherSection
	AllowManualTags bool
	TagCount          int
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
	HasIgnores          bool                      // Whether tag has IgnoreChanges entries
	IgnoreCount         int                       // Number of ignored members
	IgnoreList          []string                  // List of ignored member names
	LastPoll            string                    // Formatted timestamp of last poll
	LastChanged         string                    // Formatted timestamp of last value change
	PublishedChildren   map[string]PublishedChild // Map of child paths to their published status
	PublishedChildCount int                       // Number of published (enabled) children
}

// tagConfigEntry holds per-tag configuration extracted from PLCConfig.Tags.
type tagConfigEntry struct {
	Alias         string
	Enabled       bool
	Writable      bool
	IgnoreChanges []string
}

// repubCacheEntry caches precomputed config lookup maps for a PLC.
type repubCacheEntry struct {
	configMap    map[string]*tagConfigEntry
	childTagsMap map[string]map[string]PublishedChild
	configLen    int // len(plc.Config.Tags) at build time
	tagsLen      int // len(discovered tags) at build time
}

// getRepubCache returns cached configMap and childTagsMap for a PLC,
// rebuilding if the cache is stale or missing.
func (h *Handlers) getRepubCache(plcName string, configTags []config.TagSelection, tagNameSet map[string]bool) (map[string]*tagConfigEntry, map[string]map[string]PublishedChild) {
	configLen := len(configTags)
	tagsLen := len(tagNameSet)

	h.repubCacheMu.RLock()
	entry, ok := h.repubCache[plcName]
	h.repubCacheMu.RUnlock()

	if ok && entry.configLen == configLen && entry.tagsLen == tagsLen {
		return entry.configMap, entry.childTagsMap
	}

	// Rebuild
	configMap := make(map[string]*tagConfigEntry, len(configTags))
	childTagsMap := make(map[string]map[string]PublishedChild)

	for i := range configTags {
		sel := &configTags[i]
		configMap[sel.Name] = &tagConfigEntry{
			Alias:         sel.Alias,
			Enabled:       sel.Enabled,
			Writable:      sel.Writable,
			IgnoreChanges: sel.IgnoreChanges,
		}

		// Check if this config entry is a child of a known tag.
		// Walk backwards through dots to find the longest parent prefix.
		name := sel.Name
		for idx := len(name) - 1; idx >= 0; idx-- {
			if name[idx] == '.' {
				prefix := name[:idx]
				if tagNameSet[prefix] {
					childPath := name[idx+1:]
					if childTagsMap[prefix] == nil {
						childTagsMap[prefix] = make(map[string]PublishedChild)
					}
					childTagsMap[prefix][childPath] = PublishedChild{
						Enabled:  sel.Enabled,
						Writable: sel.Writable,
					}
					break
				}
			}
		}
	}

	h.repubCacheMu.Lock()
	h.repubCache[plcName] = &repubCacheEntry{
		configMap:    configMap,
		childTagsMap: childTagsMap,
		configLen:    configLen,
		tagsLen:      tagsLen,
	}
	h.repubCacheMu.Unlock()

	return configMap, childTagsMap
}

// invalidateRepubCache clears cached config maps for a PLC so they are
// rebuilt on the next page load.
func (h *Handlers) invalidateRepubCache(plcName string) {
	h.repubCacheMu.Lock()
	delete(h.repubCache, plcName)
	h.repubCacheMu.Unlock()
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

		connMode := plc.GetConnectionMode()
		productName := ""
		if info := plc.GetDeviceInfo(); info != nil {
			productName = info.Model
		}

		rp := RepublisherPLC{
			Name:              plc.Config.Name,
			Address:           plc.Config.Address,
			Family:            plc.Config.GetFamily().String(),
			ProductName:       productName,
			ConnectionMode:    connMode,
			Status:            status.String(),
			StatusClass:       statusClass,
			LastPoll:          lastPollStr,
			Sections:          make([]RepublisherSection, 0),
			AllowManualTags: plc.AllowManualTags(),
		}

		// Get runtime data
		tags := plc.GetTags()
		programs := plc.GetPrograms()
		values := plc.GetValues()
		var lastChanged map[string]time.Time // not tracked yet
		addressBased := plc.Config.IsAddressBased()
		isManual := !plc.Config.SupportsDiscovery()

		// Build tag name set for parent matching
		tagNameSet := make(map[string]bool, len(tags))
		for _, t := range tags {
			tagNameSet[t.Name] = true
		}

		// For non-discovery PLCs, filter out child tags that would be shown as UDT
		// members when the parent is expanded. filterStructChildren in the manager
		// only works after type resolution; this catches the pre-resolution case
		// by checking if a parent tag name exists in the tag list.
		if isManual && !addressBased {
			filtered := tags[:0:0]
			for _, t := range tags {
				if idx := strings.Index(t.Name, "."); idx > 0 {
					parent := t.Name[:idx]
					if tagNameSet[parent] {
						continue // Skip: parent exists, will be shown as child
					}
				}
				filtered = append(filtered, t)
			}
			tags = filtered
		}

		// Get config lookup maps (cached per PLC, rebuilt on config change)
		configMap, childTagsMap := h.getRepubCache(plc.Config.Name, plc.Config.Tags, tagNameSet)

		// Build program prefix set for Beckhoff-style grouping
		programPrefixes := make(map[string]bool)
		for _, prog := range programs {
			programPrefixes[prog+"."] = true
		}

		// Organize tags by section (Controller vs Program)
		controllerTags := make([]RepublisherTag, 0)
		programTags := make(map[string][]RepublisherTag)

		for _, tag := range tags {
			rt := h.buildRepublisherTag(tag.Name, tag.TypeName, configMap, childTagsMap, values, lastChanged, lastPollStr, addressBased, isManual)

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

		// Collect all program names (from discovery + extracted from tag names)
		allPrograms := make(map[string]bool)
		for _, prog := range programs {
			allPrograms[prog] = true
		}
		for prog := range programTags {
			allPrograms[prog] = true
		}
		sortedPrograms := make([]string, 0, len(allPrograms))
		for prog := range allPrograms {
			sortedPrograms = append(sortedPrograms, prog)
		}
		sort.Strings(sortedPrograms)

		// Add Program sections
		for _, prog := range sortedPrograms {
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

		// Compute total tag count
		for _, sec := range rp.Sections {
			rp.TagCount += len(sec.Tags)
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
	configMap map[string]*tagConfigEntry,
	childTagsMap map[string]map[string]PublishedChild,
	values map[string]*plcman.TagValue,
	lastChanged map[string]time.Time,
	lastPollStr string,
	addressBased bool,
	isManual bool,
) RepublisherTag {
	rt := RepublisherTag{
		Name:              tagName,
		DisplayName:       getDisplayName(tagName, addressBased, isManual),
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
		for _, child := range children {
			if child.Enabled {
				rt.PublishedChildCount++
			}
		}
	}

	// Determine struct type from type name (works even without values)
	rt.IsStruct = isStructType(rt.Type)

	// Get value data if available (JSON is lazy-loaded on demand via ensureTagValue)
	if v, ok := values[tagName]; ok && v != nil {
		goVal := v.GoValue()
		rt.Value = formatTagValue(goVal)

		// Upgrade to struct if value is a map (catches cases where type name alone isn't sufficient)
		if m, ok := goVal.(map[string]interface{}); ok {
			rt.IsStruct = true
			rt.FieldCount = len(m)
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

// getDisplayName returns the display name for a tag.
// In discovery mode, strips to the last dot segment (tree hierarchy provides context).
// In manual mode, full name for top-level tags, last dot segment for UDT children.
// Address-based PLCs always use the full name.
func getDisplayName(name string, addressBased, isManual bool) string {
	if addressBased {
		return name
	}
	if isManual {
		// Strip program prefix first
		if strings.HasPrefix(name, "Program:") {
			rest := name[8:] // len("Program:") == 8
			if idx := strings.Index(rest, "."); idx >= 0 {
				name = rest[idx+1:]
			}
		}
		// Strip to last dot for UDT children; top-level tags have no dots
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			return name[idx+1:]
		}
		return name
	}
	// Discovery mode: strip "Program:" prefix to get the tag path
	if strings.HasPrefix(name, "Program:") {
		rest := name[8:]
		if idx := strings.Index(rest, "."); idx >= 0 {
			name = rest[idx+1:]
		} else {
			name = rest
		}
	}
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
		// Logix / Micro800
		"BOOL": true, "SINT": true, "INT": true, "DINT": true, "LINT": true,
		"USINT": true, "UINT": true, "UDINT": true, "ULINT": true,
		"REAL": true, "LREAL": true, "STRING": true, "SHORT_STRING": true,
		"BYTE": true, "WORD": true, "DWORD": true, "LWORD": true,
		// S7
		"WSTRING": true, "CHAR": true, "WCHAR": true,
		"DATE": true, "TIME": true, "TIME_OF_DAY": true,
		// Beckhoff/ADS
		"LTIME": true, "DATE_AND_TIME": true, "VOID": true,
		// Go native types (from GoValue reflection)
		"bool": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"uint8": true, "uint16": true, "uint32": true, "uint64": true,
		"float32": true, "float64": true, "string": true,
		// Fallback
		"UNKNOWN": true,
	}
	// Arrays are always expandable (they have indexed children)
	if strings.HasSuffix(typeName, "[]") {
		return true
	}
	// Strip length annotation e.g. "STRING(32)" -> "STRING"
	base := typeName
	if idx := strings.IndexByte(base, '('); idx >= 0 {
		base = base[:idx]
	}
	return !basicTypes[base] && typeName != ""
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
			case kafka.StatusConnected:
				statusClass = "status-connected"
				status = "Connected"
			case kafka.StatusConnecting:
				statusClass = "status-connecting"
				status = "Connecting"
			case kafka.StatusError:
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

// RuleData holds Rule display data.
type RuleData struct {
	Name        string
	LogicMode   string
	Conditions  int
	Actions     int
	Status      string
	StatusClass string
	Enabled     bool
	FireCount   int64
	LastFire    string
}

func (h *Handlers) getRulesData() []RuleData {
	ruleMgr := h.managers.GetRuleMgr()
	if ruleMgr == nil {
		return nil
	}

	infos := ruleMgr.GetAllRuleInfo()
	result := make([]RuleData, 0, len(infos))

	for _, info := range infos {
		status := info.Status.String()
		statusClass := ruleStatusClass(info.Status)

		logicMode := string(info.LogicMode)
		if logicMode == "" {
			logicMode = string(config.RuleLogicAND)
		}

		lastFireStr := ""
		if !info.LastFire.IsZero() {
			lastFireStr = info.LastFire.Format("2006-01-02 15:04:05")
		}

		result = append(result, RuleData{
			Name:        info.Name,
			LogicMode:   logicMode,
			Conditions:  info.Conditions,
			Actions:     info.Actions,
			Status:      status,
			StatusClass: statusClass,
			Enabled:     info.Status != rule.StatusDisabled,
			FireCount:   info.FireCount,
			LastFire:    lastFireStr,
		})
	}

	// Sort by name for stable ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

func ruleStatusClass(s rule.Status) string {
	switch s {
	case rule.StatusArmed:
		return "success"
	case rule.StatusFiring:
		return "warning"
	case rule.StatusWaitingClear:
		return "warning"
	case rule.StatusCooldown:
		return "info"
	case rule.StatusError:
		return "danger"
	default:
		return "secondary"
	}
}

func (h *Handlers) getDebugLogs() []string {
	store := tui.GetDebugStore()
	if store == nil {
		return nil
	}
	messages := store.GetMessages()
	lines := make([]string, len(messages))
	for i, msg := range messages {
		ts := msg.Timestamp.Format("2006-01-02 15:04:05")
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
			Timestamp: msg.Timestamp.Format("2006-01-02 15:04:05"),
			Level:     msg.Level,
			Message:   msg.Message,
		}
	}
	return entries
}
