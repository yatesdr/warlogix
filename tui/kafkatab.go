package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlink/config"
	"warlink/kafka"
)

// KafkaTab handles the Kafka configuration tab.
type KafkaTab struct {
	app        *App
	flex       *tview.Flex
	table      *tview.Table
	tableFrame *tview.Frame
	info       *tview.TextView
	statusBar  *tview.TextView
	buttonBar  *tview.TextView
}

// NewKafkaTab creates a new Kafka tab.
func NewKafkaTab(app *App) *KafkaTab {
	t := &KafkaTab{app: app}
	t.setupUI()
	t.Refresh()
	return t
}

func (t *KafkaTab) setupUI() {
	// Button bar (themed)
	t.buttonBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	t.updateButtonBar()

	// Cluster table
	t.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	ApplyTableTheme(t.table)

	t.table.SetInputCapture(t.handleKeys)
	t.table.SetSelectedFunc(t.onSelect)

	// Set up headers (themed)
	headers := []string{"", "Name", "Brokers", "TLS", "SASL", "Status"}
	for i, h := range headers {
		t.table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(CurrentTheme.Accent).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	t.tableFrame = tview.NewFrame(t.table).SetBorders(1, 0, 0, 0, 1, 1)
	t.tableFrame.SetBorder(true).SetTitle(" Kafka Clusters ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Info panel
	t.info = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetTextColor(CurrentTheme.Text)
	t.info.SetBorder(true).SetTitle(" Cluster Info ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Content area
	content := tview.NewFlex().
		AddItem(t.tableFrame, 0, 2, true).
		AddItem(t.info, 0, 1, false)

	// Status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(CurrentTheme.Text)

	// Main layout
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.buttonBar, 1, 0, false).
		AddItem(content, 0, 1, true).
		AddItem(t.statusBar, 1, 0, false)
}

func (t *KafkaTab) handleKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	case 'a':
		t.showAddDialog()
		return nil
	case 'e':
		t.showEditDialog()
		return nil
	case 'x':
		t.removeSelected()
		return nil
	case 'c':
		t.connectSelected()
		return nil
	case 'C':
		t.disconnectSelected()
		return nil
	}
	return event
}

func (t *KafkaTab) getSelectedName() string {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return ""
	}
	cell := t.table.GetCell(row, 1)
	if cell == nil {
		return ""
	}
	return cell.Text
}

func (t *KafkaTab) onSelect(row, col int) {
	name := t.getSelectedName()
	if name == "" {
		return
	}
	t.updateInfo(name)
}

func (t *KafkaTab) updateInfo(name string) {
	cfg := t.app.config.FindKafka(name)
	if cfg == nil {
		t.info.SetText("")
		return
	}

	th := CurrentTheme
	producer := t.app.kafkaMgr.GetProducer(name)

	info := th.Label("Name", cfg.Name) + "\n"
	info += th.Label("Brokers", strings.Join(cfg.Brokers, ", ")) + "\n"
	info += fmt.Sprintf("%sTLS:%s %v\n", th.TagAccent, th.TagReset, cfg.UseTLS)
	if cfg.SASLMechanism != "" {
		info += th.Label("SASL", cfg.SASLMechanism) + "\n"
	}
	info += fmt.Sprintf("%sRequired Acks:%s %d\n", th.TagAccent, th.TagReset, cfg.RequiredAcks)
	info += fmt.Sprintf("%sMax Retries:%s %d\n", th.TagAccent, th.TagReset, cfg.MaxRetries)

	if cfg.Selector != "" {
		info += th.Label("Selector", cfg.Selector) + "\n"
	} else {
		info += th.Label("Selector", "(none)") + "\n"
	}

	// Auto-create topics (default true if nil)
	autoCreate := cfg.AutoCreateTopics == nil || *cfg.AutoCreateTopics
	if autoCreate {
		info += fmt.Sprintf("%sAuto-create Topics:%s %s\n", th.TagAccent, th.TagSuccess, "Yes")
	} else {
		info += fmt.Sprintf("%sAuto-create Topics:%s %s\n", th.TagAccent, th.TagWarning, "No")
	}

	if producer != nil {
		status := producer.GetStatus()
		info += fmt.Sprintf("\n%sStatus:%s %s\n", th.TagAccent, th.TagReset, status.String())
		if err := producer.GetError(); err != nil {
			info += fmt.Sprintf("%sError:%s %s\n", th.TagAccent, th.TagError, err.Error())
		}
		sent, errors, lastSend := producer.GetStats()
		info += fmt.Sprintf("%sMessages Sent:%s %d\n", th.TagAccent, th.TagReset, sent)
		info += fmt.Sprintf("%sErrors:%s %d\n", th.TagAccent, th.TagReset, errors)
		if !lastSend.IsZero() {
			info += fmt.Sprintf("%sLast Send:%s %s\n", th.TagAccent, th.TagReset, lastSend.Format("15:04:05"))
		}
	}

	t.info.SetText(info)
}

// GetPrimitive returns the main primitive for this tab.
func (t *KafkaTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *KafkaTab) GetFocusable() tview.Primitive {
	return t.table
}

// Refresh updates the display.
func (t *KafkaTab) Refresh() {
	// Clear existing rows (keep header)
	for t.table.GetRowCount() > 1 {
		t.table.RemoveRow(1)
	}

	// Sort clusters by name
	clusters := make([]config.KafkaConfig, len(t.app.config.Kafka))
	copy(clusters, t.app.config.Kafka)
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Name < clusters[j].Name
	})

	// Add clusters to table
	for i, cfg := range clusters {
		row := i + 1

		// Status indicator (themed)
		indicator := CurrentTheme.StatusDisconnected
		producer := t.app.kafkaMgr.GetProducer(cfg.Name)
		if producer != nil {
			switch producer.GetStatus() {
			case kafka.StatusConnected:
				indicator = CurrentTheme.StatusConnected
			case kafka.StatusConnecting:
				indicator = CurrentTheme.StatusConnecting
			case kafka.StatusError:
				indicator = CurrentTheme.StatusError
			}
		}

		// TLS indicator
		tlsStr := "No"
		if cfg.UseTLS {
			tlsStr = "Yes"
		}

		// SASL indicator
		saslStr := "-"
		if cfg.SASLMechanism != "" {
			saslStr = cfg.SASLMechanism
		}

		// Brokers (truncated)
		brokers := strings.Join(cfg.Brokers, ", ")
		if len(brokers) > 30 {
			brokers = brokers[:27] + "..."
		}

		// Status text
		statusText := "Disabled"
		if cfg.Enabled {
			if producer != nil {
				statusText = producer.GetStatus().String()
			}
		}

		t.table.SetCell(row, 0, tview.NewTableCell(indicator).SetExpansion(0))
		t.table.SetCell(row, 1, tview.NewTableCell(cfg.Name).SetExpansion(1))
		t.table.SetCell(row, 2, tview.NewTableCell(brokers).SetExpansion(2))
		t.table.SetCell(row, 3, tview.NewTableCell(tlsStr).SetExpansion(0))
		t.table.SetCell(row, 4, tview.NewTableCell(saslStr).SetExpansion(1))
		t.table.SetCell(row, 5, tview.NewTableCell(statusText).SetExpansion(1))
	}

	// Update status bar
	connected := 0
	for _, cfg := range clusters {
		producer := t.app.kafkaMgr.GetProducer(cfg.Name)
		if producer != nil && producer.GetStatus() == kafka.StatusConnected {
			connected++
		}
	}
	t.statusBar.SetText(fmt.Sprintf(" %d clusters, %d connected", len(clusters), connected))

	// Update info panel for selected
	if name := t.getSelectedName(); name != "" {
		t.updateInfo(name)
	}
}

func (t *KafkaTab) showAddDialog() {
	const pageName = "add-kafka"

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Add Kafka Cluster ")

	form.AddInputField("Name:", "", 30, nil, nil)
	form.AddInputField("Brokers:", "localhost:9092", 30, nil, nil)
	form.AddCheckbox("Use TLS:", false, nil)
	form.AddDropDown("SASL:", []string{"None", "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"}, 0, nil)
	form.AddInputField("Username:", "", 30, nil, nil)
	form.AddPasswordField("Password:", "", 30, '*', nil)
	form.AddInputField("Selector:", "", 30, nil, nil)
	form.AddCheckbox("Auto-create Topics:", true, nil)
	form.AddCheckbox("Auto-connect:", false, nil)

	form.AddButton("Add", func() {
		name := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		brokers := form.GetFormItemByLabel("Brokers:").(*tview.InputField).GetText()
		useTLS := form.GetFormItemByLabel("Use TLS:").(*tview.Checkbox).IsChecked()
		saslIdx, _ := form.GetFormItemByLabel("SASL:").(*tview.DropDown).GetCurrentOption()
		username := form.GetFormItemByLabel("Username:").(*tview.InputField).GetText()
		password := form.GetFormItemByLabel("Password:").(*tview.InputField).GetText()
		selector := form.GetFormItemByLabel("Selector:").(*tview.InputField).GetText()
		autoCreateTopics := form.GetFormItemByLabel("Auto-create Topics:").(*tview.Checkbox).IsChecked()
		autoConnect := form.GetFormItemByLabel("Auto-connect:").(*tview.Checkbox).IsChecked()

		if name == "" || brokers == "" {
			t.app.showError("Error", "Name and brokers are required")
			return
		}

		saslMechs := []string{"", "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"}
		autoCreatePtr := autoCreateTopics
		cfg := config.KafkaConfig{
			Name:             name,
			Enabled:          autoConnect,
			Brokers:          strings.Split(brokers, ","),
			UseTLS:           useTLS,
			SASLMechanism:    saslMechs[saslIdx],
			Username:         username,
			Password:         password,
			RequiredAcks:     -1,
			MaxRetries:       3,
			PublishChanges:   true, // Always enabled, per-tag control available
			Selector:         selector,
			AutoCreateTopics: &autoCreatePtr,
		}

		// Trim whitespace from brokers
		for i := range cfg.Brokers {
			cfg.Brokers[i] = strings.TrimSpace(cfg.Brokers[i])
		}

		t.app.LockConfig()
		t.app.config.AddKafka(cfg)
		t.app.UnlockAndSaveConfig()

		// Add to manager
		t.app.kafkaMgr.AddCluster(&kafka.Config{
			Name:             cfg.Name,
			Enabled:          cfg.Enabled,
			Brokers:          cfg.Brokers,
			UseTLS:           cfg.UseTLS,
			SASLMechanism:    kafka.SASLMechanism(cfg.SASLMechanism),
			Username:         cfg.Username,
			Password:         cfg.Password,
			RequiredAcks:     cfg.RequiredAcks,
			MaxRetries:       cfg.MaxRetries,
			PublishChanges:   cfg.PublishChanges,
			Selector:         cfg.Selector,
			AutoCreateTopics: autoCreateTopics,
		}, t.app.config.Namespace)

		if autoConnect {
			go t.app.kafkaMgr.Connect(name)
		}

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Added Kafka cluster: %s", name))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 55, 24, func() {
		t.app.closeModal(pageName)
	})
}

func (t *KafkaTab) showEditDialog() {
	const pageName = "edit-kafka"

	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindKafka(name)
	if cfg == nil {
		return
	}

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Edit Kafka Cluster ")

	saslIdx := 0
	switch cfg.SASLMechanism {
	case "PLAIN":
		saslIdx = 1
	case "SCRAM-SHA-256":
		saslIdx = 2
	case "SCRAM-SHA-512":
		saslIdx = 3
	}

	// Default to true if not set
	currentAutoCreate := cfg.AutoCreateTopics == nil || *cfg.AutoCreateTopics

	form.AddInputField("Name:", cfg.Name, 30, nil, nil)
	form.AddInputField("Brokers:", strings.Join(cfg.Brokers, ", "), 30, nil, nil)
	form.AddCheckbox("Use TLS:", cfg.UseTLS, nil)
	form.AddDropDown("SASL:", []string{"None", "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"}, saslIdx, nil)
	form.AddInputField("Username:", cfg.Username, 30, nil, nil)
	form.AddPasswordField("Password:", cfg.Password, 30, '*', nil)
	form.AddInputField("Selector:", cfg.Selector, 30, nil, nil)
	form.AddCheckbox("Auto-create Topics:", currentAutoCreate, nil)
	form.AddCheckbox("Auto-connect:", cfg.Enabled, nil)

	originalName := cfg.Name

	form.AddButton("Save", func() {
		newName := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		brokers := form.GetFormItemByLabel("Brokers:").(*tview.InputField).GetText()
		useTLS := form.GetFormItemByLabel("Use TLS:").(*tview.Checkbox).IsChecked()
		newSaslIdx, _ := form.GetFormItemByLabel("SASL:").(*tview.DropDown).GetCurrentOption()
		username := form.GetFormItemByLabel("Username:").(*tview.InputField).GetText()
		password := form.GetFormItemByLabel("Password:").(*tview.InputField).GetText()
		selector := form.GetFormItemByLabel("Selector:").(*tview.InputField).GetText()
		autoCreateTopics := form.GetFormItemByLabel("Auto-create Topics:").(*tview.Checkbox).IsChecked()
		autoConnect := form.GetFormItemByLabel("Auto-connect:").(*tview.Checkbox).IsChecked()

		if newName == "" || brokers == "" {
			t.app.showError("Error", "Name and brokers are required")
			return
		}

		saslMechs := []string{"", "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"}
		autoCreatePtr := autoCreateTopics
		updated := config.KafkaConfig{
			Name:             newName,
			Enabled:          autoConnect,
			Brokers:          strings.Split(brokers, ","),
			UseTLS:           useTLS,
			SASLMechanism:    saslMechs[newSaslIdx],
			Username:         username,
			Password:         password,
			RequiredAcks:     cfg.RequiredAcks,
			MaxRetries:       cfg.MaxRetries,
			PublishChanges:   true, // Always enabled, per-tag control available
			Selector:         selector,
			AutoCreateTopics: &autoCreatePtr,
		}

		// Trim whitespace from brokers
		for i := range updated.Brokers {
			updated.Brokers[i] = strings.TrimSpace(updated.Brokers[i])
		}

		t.app.LockConfig()
		t.app.config.UpdateKafka(originalName, updated)
		t.app.UnlockAndSaveConfig()

		// Update manager
		t.app.kafkaMgr.RemoveCluster(originalName)
		t.app.kafkaMgr.AddCluster(&kafka.Config{
			Name:             updated.Name,
			Enabled:          updated.Enabled,
			Brokers:          updated.Brokers,
			UseTLS:           updated.UseTLS,
			SASLMechanism:    kafka.SASLMechanism(updated.SASLMechanism),
			Username:         updated.Username,
			Password:         updated.Password,
			RequiredAcks:     updated.RequiredAcks,
			MaxRetries:       updated.MaxRetries,
			PublishChanges:   updated.PublishChanges,
			Selector:         updated.Selector,
			AutoCreateTopics: autoCreateTopics,
		}, t.app.config.Namespace)

		if autoConnect {
			go t.app.kafkaMgr.Connect(newName)
		}

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Updated Kafka cluster: %s", newName))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 55, 24, func() {
		t.app.closeModal(pageName)
	})
}

func (t *KafkaTab) removeSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	t.app.showConfirm("Remove Kafka Cluster", fmt.Sprintf("Remove %s?", name), func() {
		t.app.kafkaMgr.Disconnect(name)
		t.app.kafkaMgr.RemoveCluster(name)
		t.app.LockConfig()
		t.app.config.RemoveKafka(name)
		t.app.UnlockAndSaveConfig()
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Removed Kafka cluster: %s", name))
	})
}

func (t *KafkaTab) connectSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindKafka(name)
	if cfg == nil {
		return
	}

	t.app.LockConfig()
	cfg.Enabled = true
	t.app.UnlockAndSaveConfig()

	t.app.setStatus(fmt.Sprintf("Connecting to %s...", name))
	go func() {
		err := t.app.kafkaMgr.Connect(name)
		t.app.QueueUpdateDraw(func() {
			if err != nil {
				t.app.setStatus(fmt.Sprintf("Kafka connection failed: %v", err))
				DebugLogError("Kafka %s connection failed: %v", name, err)
			} else {
				t.app.setStatus(fmt.Sprintf("Connected to Kafka: %s", name))
				DebugLog("Kafka %s connected", name)
				// Force publish all values if PublishChanges is enabled
				if cfg.PublishChanges {
					go t.app.ForcePublishAllValuesToKafka()
				}
			}
			t.Refresh()
		})
	}()
}

func (t *KafkaTab) disconnectSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	t.app.LockConfig()
	cfg := t.app.config.FindKafka(name)
	if cfg != nil {
		cfg.Enabled = false
		t.app.UnlockAndSaveConfig()
	} else {
		t.app.UnlockConfig()
	}

	t.app.kafkaMgr.Disconnect(name)
	t.Refresh()
	t.app.setStatus(fmt.Sprintf("Disconnected from Kafka: %s", name))
}

func (t *KafkaTab) updateButtonBar() {
	th := CurrentTheme
	buttonText := " " + th.TagHotkey + "a" + th.TagActionText + "dd  " +
		th.TagHotkey + "e" + th.TagActionText + "dit  " +
		th.TagHotkey + "x" + th.TagActionText + " remove  " +
		th.TagHotkey + "c" + th.TagActionText + "onnect  dis" +
		th.TagHotkey + "C" + th.TagActionText + "onnect  " +
		th.TagActionText + "│  " +
		th.TagHotkey + "?" + th.TagActionText + " help  " +
		th.TagHotkey + "Shift+Tab" + th.TagActionText + " next tab " + th.TagReset
	t.buttonBar.SetText(buttonText)
}

// RefreshTheme updates theme-dependent UI elements.
func (t *KafkaTab) RefreshTheme() {
	t.updateButtonBar()
	th := CurrentTheme
	t.tableFrame.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.info.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.info.SetTextColor(th.Text)
	t.statusBar.SetTextColor(th.Text)
	ApplyTableTheme(t.table)
	// Update header colors
	for i := 0; i < t.table.GetColumnCount(); i++ {
		if cell := t.table.GetCell(0, i); cell != nil {
			cell.SetTextColor(th.Accent)
		}
	}
}
